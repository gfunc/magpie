package gateway

import (
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// registryDefaultEffort is the thinking level MAGPIE_REGISTRY_DEFAULT_EFFORT
// offers as every able model's default_effort in the registry ("max", say):
// a client like kimi-code starts a model there instead of on the middle of
// its levels. "" (unset) offers none, and a level the model doesn't take is
// checked away by the caller.
func registryDefaultEffort() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("MAGPIE_REGISTRY_DEFAULT_EFFORT")))
}

// apiJSONBuckets are the registry's entries, one per wire the gateway can
// be imported on: each becomes a [providers] entry of the client that
// imports the gateway as a custom registry, a model appearing under a
// bucket only when the gateway passes it through on that wire
// untranslated, and the routing groups on the OpenAI one alone.
var apiJSONBuckets = []struct {
	id, name, typ string
	api           func(base string) string
	native        string
}{
	{"magpie-openai", "Magpie (OpenAI Chat)", "openai", func(b string) string { return b + "/v1" }, "/v1/chat/completions"},
	{"magpie-anthropic", "Magpie (Anthropic Messages)", "anthropic", func(b string) string { return b }, "/v1/messages"},
	{"magpie-openai-responses", "Magpie (OpenAI Responses)", "openai_responses", func(b string) string { return b + "/v1" }, "/v1/responses"},
	{"magpie-group", "Magpie (Routing groups)", "openai", func(b string) string { return b + "/v1" }, ""},
}

// apiJSON answers GET /api.json with the gateway as a models.dev-shaped
// custom registry (kimi-code's /provider paste, refreshed by it at startup):
// id, name, api and type per bucket, and per model the fields that importer
// reads — id kept in magpie's provider/model form, unknown window sizes and
// prices left out rather than invented. The api base is MAGPIE_PUBLIC_URL
// when set, else the address the registry was fetched on.
func (s *Server) apiJSON(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimSuffix(os.Getenv("MAGPIE_PUBLIC_URL"), "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	var price func(provider.Entry) (catalog.Price, bool)
	if local(r) || sharedWith(r) {
		st, find := settings.Load(), provider.GroupFinder()
		price = func(e provider.Entry) (catalog.Price, bool) { return provider.EntryPriceIn(st, find, e) }
	} else {
		price = func(provider.Entry) (catalog.Price, bool) { return catalog.Price{}, false }
	}
	bucketEntries := make([][]provider.Entry, len(apiJSONBuckets))
	for _, e := range catalogFor(r) {
		if e.Group != "" {
			bucketEntries[len(apiJSONBuckets)-1] = append(bucketEntries[len(apiJSONBuckets)-1], e)
			continue
		}
		native := provider.NativeEndpoints(e)
		for i, b := range apiJSONBuckets {
			if b.native != "" && slices.Contains(native, b.native) {
				bucketEntries[i] = append(bucketEntries[i], e)
			}
		}
	}
	out := map[string]any{}
	for i, b := range apiJSONBuckets {
		models := map[string]any{}
		labels := provider.Labels(bucketEntries[i])
		for j, e := range bucketEntries[i] {
			// the registry's key and its id agree: a group's bare id
			// (models.dev keeps a model's id equal to its key)
			if e.Group != "" {
				e.ID = strings.TrimPrefix(e.ID, "group/")
			}
			models[e.ID] = apiJSONModel(e, labels[j], price)
		}
		out[b.id] = map[string]any{"id": b.id, "name": b.name, "api": b.api(base), "type": b.typ, "models": models}
	}
	writeJSON(w, 200, out)
}

// apiJSONModel is one model of the registry, in the shape the importer
// reads: name (the agent-list label, its provider's after it, as Labels
// names the models of an agent's own list — honoring the plain-names
// settings, and disambiguating same-named models within the bucket),
// limit (context and output when known), tool_call, reasoning and its
// efforts (a group's own: the catalog computes them over the live
// members, the intersection of their levels, a fixed member's level, or
// the group's Levels, as the gateway serves them), the modalities as
// magpie lists them, and cost when a price is known. Fields magpie
// doesn't know are omitted, so the importer fills its own defaults
// instead of trusting an invented number.
func apiJSONModel(e provider.Entry, name string, price func(provider.Entry) (catalog.Price, bool)) map[string]any {
	m := map[string]any{"id": e.ID, "tool_call": true}
	if name != "" {
		m["name"] = name
	}
	limit := map[string]any{}
	if e.Context > 0 {
		limit["context"] = e.Context
	}
	if out := e.PublishedOutput(); out > 0 {
		limit["output"] = out
	}
	if len(limit) > 0 {
		m["limit"] = limit
	}
	if e.Reasoning {
		m["reasoning"] = true
	}
	if len(e.Efforts) > 0 {
		m["support_efforts"] = e.Efforts
		// kimi-code starts a model on default_effort, else the middle of
		// support_efforts, else on; the registry's owner picks another
		// level (max, say) through the environment, and it goes out only
		// where the model takes it
		if de := registryDefaultEffort(); slices.Contains(e.Efforts, de) {
			m["default_effort"] = de
		}
	}
	if in, out := e.Modalities(); in != nil || out != nil {
		mod := map[string]any{}
		if in != nil {
			mod["input"] = in
		}
		if out != nil {
			mod["output"] = out
		}
		m["modalities"] = mod
	}
	if pr, ok := price(e); ok {
		m["cost"] = pr
	}
	return m
}

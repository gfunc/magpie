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
	bucketModels := make([]map[string]any, len(apiJSONBuckets))
	for i := range bucketModels {
		bucketModels[i] = map[string]any{}
	}
	for _, e := range catalogFor(r) {
		if e.Group != "" {
			id := strings.TrimPrefix(e.ID, "group/")
			bucketModels[len(apiJSONBuckets)-1][id] = apiJSONModel(e, false, price)
			continue
		}
		native := nativeEndpoints(e)
		for i, b := range apiJSONBuckets {
			if b.native != "" && slices.Contains(native, b.native) {
				bucketModels[i][e.ID] = apiJSONModel(e, true, price)
			}
		}
	}
	out := map[string]any{}
	for i, b := range apiJSONBuckets {
		out[b.id] = map[string]any{"id": b.id, "name": b.name, "api": b.api(base), "type": b.typ, "models": bucketModels[i]}
	}
	writeJSON(w, 200, out)
}

// apiJSONModel is one model of the registry, in the shape the importer
// reads: name, limit (context and output when known), tool_call, reasoning
// and its efforts (a group's left out, as its membership varies), the
// modalities as magpie lists them, and cost when a price is known. Fields
// magpie doesn't know are omitted, so the importer fills its own defaults
// instead of trusting an invented number.
func apiJSONModel(e provider.Entry, efforts bool, price func(provider.Entry) (catalog.Price, bool)) map[string]any {
	m := map[string]any{"id": e.ID, "tool_call": true}
	if n := e.PlainName(); n != "" {
		m["name"] = n
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
	if efforts && len(e.Efforts) > 0 {
		m["support_efforts"] = e.Efforts
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

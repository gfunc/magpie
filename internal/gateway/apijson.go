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

// registryDefaultEffort is the default thinking level every able model
// of the registry is offered: the setting's value first, else what
// MAGPIE_REGISTRY_DEFAULT_EFFORT says ("max", say) — a client like
// kimi-code starts a model there instead of on the middle of its levels.
// "" (neither set) offers none, and a level the model doesn't take is
// checked away by the caller.
func registryDefaultEffort(st settings.Settings) string {
	if de := strings.ToLower(strings.TrimSpace(st.RegistryEffort)); de != "" {
		return de
	}
	return strings.ToLower(strings.TrimSpace(os.Getenv("MAGPIE_REGISTRY_DEFAULT_EFFORT")))
}

// apiJSONWires are the registry's wire entries, one per protocol the
// gateway can be imported on: each becomes a [providers] entry of the
// client that imports the gateway as a custom registry, a model appearing
// under a wire only when the gateway passes it through on that protocol
// untranslated. Gemini's wire (type google-genai, the importer's own
// string, api at the /v1beta root Google's SDKs build their paths on) is
// among them; its path is per model, so membership here is by protocol,
// never by path. The routing groups are not a wire: they ride their own
// entry, apiJSONGroups, by their own switch.
var apiJSONWires = []struct {
	id, name, typ string
	proto         provider.Protocol
	api           func(string) string
}{
	{"magpie-openai", "Magpie (OpenAI Chat)", "openai", provider.Chat, func(b string) string { return b + "/v1" }},
	{"magpie-anthropic", "Magpie (Anthropic Messages)", "anthropic", provider.Anthropic, func(b string) string { return b }},
	{"magpie-openai-responses", "Magpie (OpenAI Responses)", "openai_responses", provider.Responses, func(b string) string { return b + "/v1" }},
	{"magpie-gemini", "Magpie (Google GenAI)", "google-genai", provider.Gemini, func(b string) string { return b + "/v1beta" }},
}

// apiJSONGroups is the registry's routing-groups entry: the groups ride
// the OpenAI-compatible API a group is served on, listed by their own
// switch (settings.RegistryGroupsOn) rather than a wire's tick, under
// settings' own id for them — the one constant the two share, so the
// gateway and the settings page cannot drift apart.
var apiJSONGroups = struct {
	id, name, typ string
	api           func(string) string
}{
	settings.RegistryGroupID, "Magpie (Routing groups)", "openai",
	func(b string) string { return b + "/v1" },
}

// apiJSON answers GET /api.json with the gateway as a models.dev-shaped
// custom registry (kimi-code's /provider paste, refreshed by it at startup):
// id, name, api and type per wire, and per model the fields that importer
// reads — id kept in magpie's provider/model form, unknown window sizes and
// prices left out rather than invented. The api base is MAGPIE_PUBLIC_URL
// when set, else the address the registry was fetched on. The registry is
// off until the user serves it (RegistryOn): off, the endpoint answers
// 404 before it walks the catalog, and on, one Shared read carries the
// switch, the served wires, the routing groups' own switch and the
// default effort. The wires go out first, the routing groups' entry last.
func (s *Server) apiJSON(w http.ResponseWriter, r *http.Request) {
	st := settings.Shared()
	if !st.RegistryOn {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "registry disabled"})
		return
	}
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
		find := provider.GroupFinder()
		price = func(e provider.Entry) (catalog.Price, bool) { return provider.EntryPriceIn(st, find, e) }
	} else {
		price = func(provider.Entry) (catalog.Price, bool) { return catalog.Price{}, false }
	}
	de := registryDefaultEffort(st)
	wireEntries := make([][]provider.Entry, len(apiJSONWires))
	var groupEntries []provider.Entry
	for _, e := range catalogFor(r) {
		if e.Group != "" {
			groupEntries = append(groupEntries, e)
			continue
		}
		protos := provider.NativeProtocols(e)
		for i, w := range apiJSONWires {
			if slices.Contains(protos, w.proto) {
				wireEntries[i] = append(wireEntries[i], e)
			}
		}
	}
	out := map[string]any{}
	for i, w := range apiJSONWires {
		if !slices.Contains(st.RegistryBuckets, w.id) {
			continue
		}
		out[w.id] = map[string]any{"id": w.id, "name": w.name, "api": w.api(base), "type": w.typ, "models": apiJSONModels(wireEntries[i], price, de)}
	}
	if settings.RegistryGroupsOn(st) {
		g := apiJSONGroups
		out[g.id] = map[string]any{"id": g.id, "name": g.name, "api": g.api(base), "type": g.typ, "models": apiJSONModels(groupEntries, price, de)}
	}
	ordered := struct {
		OpenAI          any `json:"magpie-openai,omitempty"`
		Anthropic       any `json:"magpie-anthropic,omitempty"`
		OpenAIResponses any `json:"magpie-openai-responses,omitempty"`
		Gemini          any `json:"magpie-gemini,omitempty"`
		Groups          any `json:"magpie-group,omitempty"`
	}{
		OpenAI:          out["magpie-openai"],
		Anthropic:       out["magpie-anthropic"],
		OpenAIResponses: out["magpie-openai-responses"],
		Gemini:          out["magpie-gemini"],
		Groups:          out[settings.RegistryGroupID],
	}
	writeJSON(w, 200, ordered)
}

// apiJSONModels is one registry entry's model list: each model in the
// shape the importer reads (apiJSONModel), keyed as the registry keys it
// — a routing group by its bare id, its group/ prefix trimmed
// (models.dev keeps a model's id equal to its key).
func apiJSONModels(entries []provider.Entry, price func(provider.Entry) (catalog.Price, bool), de string) map[string]any {
	models := map[string]any{}
	labels := provider.Labels(entries)
	for i, e := range entries {
		m := apiJSONModel(e, labels[i], price, de)
		if e.Group != "" {
			id := strings.TrimPrefix(e.ID, "group/")
			m["id"] = id
			models[id] = m
			continue
		}
		models[e.ID] = m
	}
	return models
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
func apiJSONModel(e provider.Entry, name string, price func(provider.Entry) (catalog.Price, bool), defaultEffort string) map[string]any {
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
		// level (max, say) in the settings or the environment, and it
		// goes out only where the model takes it
		if slices.Contains(e.Efforts, defaultEffort) {
			m["default_effort"] = defaultEffort
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

package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// registryOn writes the settings the registry's tests read under: the
// endpoint answers 404 until the user serves it, so a test that expects a
// registry turns the switch on through Save, as the page does, and the
// optional edit changes what is served — its buckets, its effort.
func registryOn(t *testing.T, edit ...func(*settings.Settings)) {
	t.Helper()
	s := settings.Load()
	s.RegistryOn = true
	for _, e := range edit {
		e(&s)
	}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// GET /api.json is the gateway as a models.dev-shaped registry (kimi-code's
// /provider paste): a model appears under a wire only where the gateway
// passes it through on that protocol untranslated, its id keeps magpie's
// provider/model form, a window it doesn't know is left out for the
// importer's own default, and the routing groups sit under their own entry
// by bare id. The wires' union is the model list's native subset — the
// parity the registry owes /v1/models.
func TestAPIJSONServesNativeBuckets(t *testing.T) {
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Anthropic: up.URL,
		Models: []string{"m-any", "m-chat", "m-res"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m-any", Name: "Model Any", Context: 128000, Output: 4096, Efforts: []string{"low", "high"}},
		{ID: "m-chat", Context: 64000, APIs: []string{"chat"}},
		{ID: "m-res", APIs: []string{"responses"}}, // the vendor's only API this provider doesn't speak: no bucket
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "daily", Name: "Daily", Members: []string{"gx/m-any"}}); err != nil {
		t.Fatal(err)
	}
	wantPrice := catalog.Price{Input: 3, Output: 12, CacheRead: 0.3, CacheWrite: 3.75}
	if err := provider.SetModelPrice("gx/m-any", &wantPrice); err != nil {
		t.Fatal(err)
	}
	registryOn(t)

	req := httptest.NewRequest("GET", "/api.json", nil)
	req.RemoteAddr = "127.0.0.1:12345" // a local reader sees known prices
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/api.json: %d %s", rec.Code, rec.Body)
	}
	var out map[string]struct {
		Type   string                    `json:"type"`
		API    string                    `json:"api"`
		Models map[string]map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 5 {
		t.Fatalf("entries: %v", out)
	}
	for id, want := range map[string]string{
		"magpie-openai":           "openai",
		"magpie-anthropic":        "anthropic",
		"magpie-openai-responses": "openai_responses",
		"magpie-gemini":           "google-genai",
		"magpie-group":            "openai",
	} {
		if out[id].Type != want {
			t.Errorf("%s: type %q, want %q", id, out[id].Type, want)
		}
		if base := out[id].API; !strings.HasPrefix(base, "http://") {
			t.Errorf("%s: api %q not off the request's host", id, base)
		}
	}
	openai, anthropic, responses, gemini, group := out["magpie-openai"].Models, out["magpie-anthropic"].Models, out["magpie-openai-responses"].Models, out["magpie-gemini"].Models, out["magpie-group"].Models
	if len(responses) != 0 {
		t.Errorf("responses wire serves %v; want none", responses)
	}
	if len(gemini) != 0 {
		t.Errorf("gemini wire serves %v; gx speaks no Gemini: want none", gemini)
	}
	if _, ok := openai["gx/m-any"]; !ok {
		t.Errorf("openai bucket lacks gx/m-any: %v", openai)
	}
	if _, ok := anthropic["gx/m-any"]; !ok {
		t.Errorf("anthropic bucket lacks gx/m-any: %v", anthropic)
	}
	if _, ok := openai["gx/m-chat"]; !ok {
		t.Errorf("openai bucket lacks gx/m-chat: %v", openai)
	}
	if _, ok := anthropic["gx/m-chat"]; ok {
		t.Errorf("anthropic bucket serves gx/m-chat, native on chat alone: %v", anthropic)
	}
	for _, b := range []map[string]map[string]any{openai, anthropic, responses, gemini} {
		if _, ok := b["gx/m-res"]; ok {
			t.Errorf("a wire serves gx/m-res, native on nothing it speaks: %v", b)
		}
	}
	if _, ok := group["daily"]; !ok {
		t.Errorf("groups entry lacks the bare id daily: %v", group)
	}
	if _, ok := group["group/daily"]; ok {
		t.Errorf("groups entry carries the prefixed id group/daily: %v", group)
	}
	// the group's efforts come from the catalog's own computation over the
	// members (the one member's levels here), like its limits do; its name
	// is the agent-list label, "· routing group" after it
	if m := group["daily"]; m["support_efforts"] == nil {
		t.Errorf("group daily carries no support_efforts: %v", m)
	} else if got, ok := m["support_efforts"].([]any); !ok || len(got) != 2 || got[0] != "low" || got[1] != "high" {
		t.Errorf("group daily: support_efforts %v, want [low high]", m["support_efforts"])
	} else if m["id"] != "daily" {
		t.Errorf("group daily: id %v, want the bare id its key has", m["id"])
	} else if m["name"] != "Daily · routing group" {
		t.Errorf("group daily: name %v, want the labelled one", m["name"])
	}
	if cost, ok := group["daily"]["cost"].(map[string]any); !ok || cost["input"] != 3.0 || cost["output"] != 12.0 || cost["cache_read"] != 0.3 || cost["cache_write"] != 3.75 {
		t.Errorf("group daily: cost %v, want the members' shared manual price", group["daily"]["cost"])
	}
	if m := openai["gx/m-any"]; m != nil && m["name"] != "Model Any · GX" {
		t.Errorf("gx/m-any: name %v, want the labelled one", m["name"])
	}
	if m := openai["gx/m-chat"]; m != nil {
		if m["name"] != "m-chat · GX" {
			t.Errorf("gx/m-chat: name %v, want the labelled one", m["name"])
		}
	}
	if m := openai["gx/m-any"]; m == nil {
		// covered above
	} else {
		if m["tool_call"] != true {
			t.Errorf("gx/m-any: tool_call %v", m["tool_call"])
		}
		limit, ok := m["limit"].(map[string]any)
		if !ok || limit["context"] != float64(128000) || limit["output"] != float64(4096) {
			t.Errorf("gx/m-any: limit %v", m["limit"])
		}
	}
	if m := openai["gx/m-chat"]; m != nil {
		limit, ok := m["limit"].(map[string]any)
		if !ok || limit["context"] != float64(64000) {
			t.Errorf("gx/m-chat: limit %v; want its known context alone", m["limit"])
		} else if _, has := limit["output"]; has {
			t.Errorf("gx/m-chat: limit %v carries an output it doesn't know", limit)
		}
	}
}

// GET /api.json answers MAGPIE_REGISTRY_DEFAULT_EFFORT by offering that
// thinking level as every able model's default_effort — the level a client
// like kimi-code starts a model on instead of the middle of its levels —
// on every bucket, and none on a model that doesn't take it.
func TestAPIJSONDefaultEffort(t *testing.T) {
	t.Setenv("MAGPIE_REGISTRY_DEFAULT_EFFORT", "max")
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Anthropic: up.URL,
		Models: []string{"m-max", "m-nomax"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m-max", Name: "Model Max", Efforts: []string{"low", "high", "max"}},
		{ID: "m-nomax", Name: "Model NoMax", Efforts: []string{"low", "high"}},
	}); err != nil {
		t.Fatal(err)
	}
	registryOn(t)
	req := httptest.NewRequest("GET", "/api.json", nil)
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	var out map[string]struct {
		Models map[string]map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if m := out["magpie-openai"].Models["gx/m-max"]; m["default_effort"] != "max" {
		t.Errorf("gx/m-max: default_effort %v, want max", m["default_effort"])
	}
	if m := out["magpie-anthropic"].Models["gx/m-max"]; m["default_effort"] != "max" {
		t.Errorf("anthropic bucket gx/m-max: default_effort %v, want max", m["default_effort"])
	}
	if m := out["magpie-openai"].Models["gx/m-nomax"]; m["default_effort"] != nil {
		t.Errorf("gx/m-nomax: default_effort %v; it takes no max", m["default_effort"])
	}
}

// GET /api.json says no default_effort while the environment is silent:
// the client's own default (the middle of a model's levels) stands.
func TestAPIJSONNoDefaultEffortWithoutEnv(t *testing.T) {
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Anthropic: up.URL,
		Models: []string{"m-max"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m-max", Name: "Model Max", Efforts: []string{"low", "high", "max"}},
	}); err != nil {
		t.Fatal(err)
	}
	registryOn(t)
	req := httptest.NewRequest("GET", "/api.json", nil)
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	var out map[string]struct {
		Models map[string]map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if m := out["magpie-openai"].Models["gx/m-max"]; m["default_effort"] != nil {
		t.Errorf("gx/m-max: default_effort %v with the environment unset", m["default_effort"])
	}
}

// The registry is off until the user serves it: a fresh install's GET
// /api.json is a 404 whose plain body says so, turning the switch on
// serves the registry again, and turning it back off is the 404 once
// more. /v1/models, the list the client asked for a registry to feed,
// is the switch's own route untouched: it answers either way.
func TestAPIJSONOffUntilServed(t *testing.T) {
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Anthropic: up.URL, Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m1", Name: "Model One"},
	}); err != nil {
		t.Fatal(err)
	}
	registry := func() (int, string) {
		req := httptest.NewRequest("GET", "/api.json", nil)
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	models := func() int {
		req := httptest.NewRequest("GET", "/v1/models", nil)
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	// writeJSON's encoder ends every body with a newline
	const off = "{\"error\":\"registry disabled\"}\n"
	if c := models(); c != 200 {
		t.Fatalf("GET /v1/models with the registry off: %d", c)
	}
	if c, b := registry(); c != 404 || b != off {
		t.Fatalf("the registry off: %d %s, want 404 %s", c, b, off)
	}
	registryOn(t)
	if c, b := registry(); c != 200 || !strings.Contains(b, "magpie-openai") {
		t.Fatalf("the registry on: %d %.200s, want the buckets served", c, b)
	}
	if c := models(); c != 200 {
		t.Fatalf("GET /v1/models with the registry on: %d", c)
	}
	s := settings.Load()
	s.RegistryOn = false
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if c, b := registry(); c != 404 || b != off {
		t.Fatalf("the registry off again: %d %s, want 404 %s", c, b, off)
	}
}

// An unchecked wire is absent from the registry and the rest unchanged;
// the routing groups go or stay by their own switch, and turning a wire
// off never moves them, for they match on their group, not on a wire.
// A registry of the groups alone is legal: no wire ticked, the switch on.
func TestAPIJSONServesCheckedWiresOnly(t *testing.T) {
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Anthropic: up.URL, Gemini: up.URL + "/v1beta",
		Models: []string{"m-any", "m-chat"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m-any", Name: "Model Any", Efforts: []string{"low", "high"}},
		{ID: "m-chat", Context: 64000, APIs: []string{"chat"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "daily", Name: "Daily", Members: []string{"gx/m-any"}}); err != nil {
		t.Fatal(err)
	}
	registry := func() map[string]struct {
		Models map[string]map[string]any `json:"models"`
	} {
		t.Helper()
		req := httptest.NewRequest("GET", "/api.json", nil)
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("/api.json: %d %s", rec.Code, rec.Body)
		}
		var out map[string]struct {
			Models map[string]map[string]any `json:"models"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	registryOn(t, func(s *settings.Settings) {
		s.RegistryBuckets = []string{"magpie-openai", "magpie-openai-responses"}
	})
	out := registry()
	for _, id := range []string{"magpie-anthropic", "magpie-gemini"} {
		if _, ok := out[id]; ok {
			t.Errorf("%s unchecked but served: %v", id, out[id])
		}
	}
	for _, id := range []string{"magpie-openai", "magpie-openai-responses", "magpie-group"} {
		if _, ok := out[id]; !ok {
			t.Errorf("%s checked but absent: %v", id, out)
		}
	}
	if m := out["magpie-openai"].Models["gx/m-any"]; m == nil {
		t.Errorf("openai wire lacks gx/m-any: %v", out["magpie-openai"].Models)
	} else if m["default_effort"] != nil {
		t.Errorf("gx/m-any: default_effort %v with none set", m["default_effort"])
	}
	if _, ok := out["magpie-openai"].Models["gx/m-chat"]; !ok {
		t.Errorf("openai wire lacks gx/m-chat: %v", out["magpie-openai"].Models)
	}
	if _, ok := out["magpie-group"].Models["daily"]; !ok {
		t.Errorf("groups entry lacks daily: %v", out["magpie-group"].Models)
	}

	// the groups' own switch off: every wire stays, their entry alone goes
	registryOn(t, func(s *settings.Settings) {
		s.RegistryBuckets = nil // every wire back on
		off := false
		s.RegistryGroups = &off
	})
	out = registry()
	if _, ok := out["magpie-group"]; ok {
		t.Errorf("the groups switched off but served: %v", out["magpie-group"])
	}
	for _, id := range settings.RegistryWireIDs {
		if _, ok := out[id]; !ok {
			t.Errorf("%s still checked but absent: %v", id, out)
		}
	}

	// the groups alone: every wire unchecked and the switch on, a registry
	// that lists nothing but its routing groups
	registryOn(t, func(s *settings.Settings) {
		s.RegistryBuckets = []string{}
		on := true
		s.RegistryGroups = &on
	})
	out = registry()
	if len(out) != 1 {
		t.Fatalf("groups-only registry: %v, want %s alone", out, settings.RegistryGroupID)
	}
	if _, ok := out[settings.RegistryGroupID].Models["daily"]; !ok {
		t.Errorf("groups-only entry lacks daily: %v", out[settings.RegistryGroupID].Models)
	}
}

// The registry's Gemini wire is the gateway as a Google GenAI registry:
// id, name, type google-genai (the importer's own string, not "gemini")
// and the api root Google's SDKs build their paths on, base+"/v1beta". A
// model is listed on it exactly when the gateway relays its
// generateContent as it is: a Gemini-only base's model, a relay that
// speaks two wires' model on each of them, and a translated-only speaker
// not at all.
func TestAPIJSONGeminiWire(t *testing.T) {
	t.Setenv("MAGPIE_PUBLIC_URL", "https://reg.example")
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gg", Name: "GG", Key: "k",
		Gemini: up.URL + "/v1beta", Models: []string{"gemini-3-pro"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gg", up.URL+"/v1beta", []catalog.Model{
		{ID: "gemini-3-pro", Name: "Gemini 3 Pro"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "both", Name: "Both", Key: "k",
		Chat: up.URL + "/v1", Gemini: up.URL + "/v1beta", Models: []string{"glm-5.3"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("both", up.URL+"/v1", []catalog.Model{
		{ID: "glm-5.3", Name: "GLM 5.3"},
	}); err != nil {
		t.Fatal(err)
	}
	registryOn(t)
	req := httptest.NewRequest("GET", "/api.json", nil)
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/api.json: %d %s", rec.Code, rec.Body)
	}
	var out map[string]struct {
		ID     string                    `json:"id"`
		Name   string                    `json:"name"`
		Type   string                    `json:"type"`
		API    string                    `json:"api"`
		Models map[string]map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	gemini, ok := out["magpie-gemini"]
	if !ok {
		t.Fatalf("no magpie-gemini entry: %v", out)
	}
	if gemini.ID != "magpie-gemini" || gemini.Name != "Magpie (Google GenAI)" || gemini.Type != "google-genai" {
		t.Errorf("the gemini entry: id %q name %q type %q", gemini.ID, gemini.Name, gemini.Type)
	}
	if gemini.API != "https://reg.example/v1beta" {
		t.Errorf("the gemini entry's api %q, want the base with /v1beta", gemini.API)
	}
	for _, id := range []string{"gg/gemini-3-pro", "both/glm-5.3"} {
		if _, ok := gemini.Models[id]; !ok {
			t.Errorf("the gemini wire lacks %s: %v", id, gemini.Models)
		}
	}
	if _, ok := gemini.Models["fake/m1"]; ok {
		t.Errorf("the gemini wire serves fake/m1, native on chat alone: %v", gemini.Models)
	}
	if m := out["magpie-openai"].Models; m["both/glm-5.3"] == nil || m["gg/gemini-3-pro"] != nil {
		t.Errorf("the openai wire: a two-wire model on it, a Gemini-only one not: %v", m)
	}
}

// A fresh settings file lists every wire and the routing groups: the
// registry a new install serves is the whole gateway, nothing trimmed by
// a list no one wrote.
func TestAPIJSONDefaultsListEveryWireAndGroups(t *testing.T) {
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m1", Name: "Model One"},
	}); err != nil {
		t.Fatal(err)
	}
	registryOn(t)
	req := httptest.NewRequest("GET", "/api.json", nil)
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/api.json: %d %s", rec.Code, rec.Body)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	want := append(slices.Clone(settings.RegistryWireIDs), settings.RegistryGroupID)
	if len(out) != len(want) {
		t.Fatalf("a fresh registry serves %d entries (%v), want %v", len(out), out, want)
	}
	for _, id := range want {
		if _, ok := out[id]; !ok {
			t.Errorf("a fresh registry lacks %s: %v", id, out)
		}
	}
	body := rec.Body.String()
	last := -1
	for _, id := range want {
		at := strings.Index(body, `"`+id+`":`)
		if at < 0 {
			t.Fatalf("the raw registry lacks the %s key: %s", id, body)
		}
		if at <= last {
			t.Fatalf("registry keys are not in wire order with groups last (%v): %s", want, body)
		}
		last = at
	}
}

// The default effort the registry offers is the setting's value, the
// environment's only while the setting is empty, and neither set offers
// none — the settings win, and a model that doesn't take the level is
// left to the client's own middle.
func TestAPIJSONEffortSettingOutranksEnv(t *testing.T) {
	t.Setenv("MAGPIE_REGISTRY_DEFAULT_EFFORT", "high")
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Anthropic: up.URL, Models: []string{"m-max", "m-mid"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m-max", Name: "Model Max", Efforts: []string{"low", "high", "max"}},
		{ID: "m-mid", Name: "Model Mid", Efforts: []string{"low", "high"}},
	}); err != nil {
		t.Fatal(err)
	}
	registry := func() map[string]map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/api.json", nil)
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("/api.json: %d %s", rec.Code, rec.Body)
		}
		var out map[string]struct {
			Models map[string]map[string]any `json:"models"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out["magpie-openai"].Models
	}

	// the setting wins over the environment's high, and a model without
	// the level carries none
	registryOn(t, func(s *settings.Settings) { s.RegistryEffort = "max" })
	models := registry()
	if m := models["gx/m-max"]; m["default_effort"] != "max" {
		t.Errorf("gx/m-max: default_effort %v, want the setting's max over the env's high", m["default_effort"])
	}
	if m := models["gx/m-mid"]; m["default_effort"] != nil {
		t.Errorf("gx/m-mid: default_effort %v; it takes no max", m["default_effort"])
	}

	// the setting empty: the environment's level applies
	registryOn(t, func(s *settings.Settings) { s.RegistryEffort = "" })
	models = registry()
	if m := models["gx/m-max"]; m["default_effort"] != "high" {
		t.Errorf("gx/m-max: default_effort %v, want the env's high while the setting is empty", m["default_effort"])
	}
	if m := models["gx/m-mid"]; m["default_effort"] != "high" {
		t.Errorf("gx/m-mid: default_effort %v, want the env's high", m["default_effort"])
	}

	// neither set: none offered, and TestAPIJSONNoDefaultEffortWithoutEnv
	// holds the shape with the environment silent too
	registryOn(t, func(s *settings.Settings) { s.RegistryEffort = "" })
	t.Setenv("MAGPIE_REGISTRY_DEFAULT_EFFORT", "")
	if m := registry()["gx/m-max"]; m["default_effort"] != nil {
		t.Errorf("gx/m-max: default_effort %v with neither set", m["default_effort"])
	}
}

// The registry's own route is a read: a read-only key reaches it like
// the usage and quota reads it keeps, served when the registry is on and
// the plain 404 when it is off, never refused for the key's read-only
// hold. The prices stay a local or shared reader's: an unauthenticated
// remote reader gets none, the gate local(r) || sharedWith(r) the
// existing price checks hold.
func TestAPIJSONReadOnlyKeyReachesRegistry(t *testing.T) {
	fresh(t)
	f := &fake{ctype: "application/json", reply: `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "gx", Name: "GX", Key: "k",
		Chat: up.URL + "/v1", Anthropic: up.URL, Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("gx", up.URL+"/v1", []catalog.Model{
		{ID: "m1", Name: "Model One"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelPrice("gx/m1", &catalog.Price{Input: 3, Output: 12}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Desk display")
	if _, err := access.Update("access-key", access.Change{Key: keys[0].ID, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	registry := func(from string) (int, string) {
		req := httptest.NewRequest("GET", "/api.json", nil)
		req.RemoteAddr = from
		req.Header.Set("Authorization", "Bearer "+secrets[0])
		rec := httptest.NewRecorder()
		lanGuard(New().Handler()).ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if c, b := registry("192.168.1.9:5000"); c != 404 || b != "{\"error\":\"registry disabled\"}\n" {
		t.Fatalf("read-only key, registry off: %d %s", c, b)
	}
	registryOn(t)
	for _, from := range []string{"127.0.0.1:5000", "192.168.1.9:5000"} {
		if c, b := registry(from); c != 200 || !strings.Contains(b, "gx/m1") {
			t.Fatalf("read-only key from %s: %d %.200s", from, c, b)
		}
	}
	// the prices stay a local or shared reader's: a reader that is
	// neither — off-machine and unauthenticated, what lanGuard refuses
	// on the gateway's own port — is served the registry with no cost in
	// it when it reaches the endpoint (a page or plugin port that passes
	// it through), the gate local(r) || sharedWith(r) keeps
	req := httptest.NewRequest("GET", "/api.json", nil)
	req.RemoteAddr = "192.168.1.9:5000"
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "gx/m1") {
		t.Fatalf("unauthenticated remote reader: %d %.200s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), `"cost"`) {
		t.Errorf("an unauthenticated remote reader is told a price: %.200s", rec.Body)
	}
}

package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

// GET /api.json is the gateway as a models.dev-shaped registry (kimi-code's
// /provider paste): a model appears under a protocol bucket only where the
// gateway passes it through untranslated, its id keeps magpie's
// provider/model form, a window it doesn't know is left out for the
// importer's own default, and the routing groups sit under their own bucket
// by bare id. The buckets' union is the model list's native subset — the
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
	if len(out) != 4 {
		t.Fatalf("buckets: %v", out)
	}
	for id, want := range map[string]string{
		"magpie-openai":           "openai",
		"magpie-anthropic":        "anthropic",
		"magpie-openai-responses": "openai_responses",
		"magpie-group":            "openai",
	} {
		if out[id].Type != want {
			t.Errorf("%s: type %q, want %q", id, out[id].Type, want)
		}
		if base := out[id].API; !strings.HasPrefix(base, "http://") {
			t.Errorf("%s: api %q not off the request's host", id, base)
		}
	}
	openai, anthropic, responses, group := out["magpie-openai"].Models, out["magpie-anthropic"].Models, out["magpie-openai-responses"].Models, out["magpie-group"].Models
	if len(responses) != 0 {
		t.Errorf("responses bucket serves %v; want none", responses)
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
	for _, b := range []map[string]map[string]any{openai, anthropic, responses} {
		if _, ok := b["gx/m-res"]; ok {
			t.Errorf("a bucket serves gx/m-res, native on nothing it speaks: %v", b)
		}
	}
	if _, ok := group["daily"]; !ok {
		t.Errorf("group bucket lacks the bare id daily: %v", group)
	}
	if _, ok := group["group/daily"]; ok {
		t.Errorf("group bucket carries the prefixed id group/daily: %v", group)
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

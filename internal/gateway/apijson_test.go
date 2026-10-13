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
		{ID: "m-any", Context: 128000, Output: 4096},
		{ID: "m-chat", Context: 64000, APIs: []string{"chat"}},
		{ID: "m-res", APIs: []string{"responses"}}, // the vendor's only API this provider doesn't speak: no bucket
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "daily", Name: "Daily", Members: []string{"gx/m-any"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api.json", nil)
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

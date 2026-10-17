package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/usage"
)

// deepSeekSite is a fake of DeepSeek's tools APIs: /v1/tools/search answers
// with the results the docs show (api-docs.deepseek.com/api/search), /v1/models
// lists a model, and /anthropic/v1/messages answers a passed-through web_search
// as DeepSeek's Anthropic API does. mode says what the search answers; raw, a
// whole reply of the test's own. Everything it was asked is recorded under a
// lock: the handler runs on many goroutines.
type deepSeekSite struct {
	*httptest.Server
	mu        sync.Mutex
	asked     []string // every path, in order
	searches  []string // "auth | ua | query | count,need_content | ori,local"
	mode      string   // "", "empty", a status, or "big"
	raw       string   // a whole search reply the test wrote
	refuseKey string   // the only key the mode refuses, "" for all of them
}

func newDeepSeekSite(t *testing.T) *deepSeekSite {
	d := &deepSeekSite{}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/anthropic/v1/messages" || r.URL.Path == "/v1/tools/search" {
			d.mu.Lock()
			d.asked = append(d.asked, r.URL.Path)
			d.mu.Unlock()
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			io.WriteString(w, `{"data":[{"id":"deepseek-chat"}]}`)
			return
		case r.URL.Path == "/v1/tools/search":
			var q map[string]any
			json.Unmarshal(b, &q)
			d.mu.Lock()
			d.searches = append(d.searches, strings.Join([]string{
				r.Header.Get("Authorization"), r.Header.Get("User-Agent"),
				fmt.Sprint(q["query"]), fmt.Sprint(q["count"]) + "," + fmt.Sprint(q["need_content"]),
				fmt.Sprint(q["ori_question"]) + "," + fmt.Sprint(q["localization"]),
			}, " | "))
			mode, raw := d.mode, d.raw
			refuseKey := d.refuseKey
			d.mu.Unlock()
			if refuseKey != "" && r.Header.Get("Authorization") != "Bearer "+refuseKey {
				mode = ""
			}
			switch {
			case raw != "":
				io.WriteString(w, raw)
			case mode == "":
				// shaped like the docs' example, with a second result that
				// carries only a snippet and a third without a URL
				io.WriteString(w, `{"results":[`+
					`{"rerank_score":2.4,"title":"DeepSeek V4 model released","url":"https://example.com/deepseek-v4",`+
					`"snippet":"DeepSeek V4 is now available, with tool calling and long-context support.",`+
					`"summary":"DeepSeek has released the V4 model. The release adds improved tool calling and long-context support for agent workflows.",`+
					`"content":"DeepSeek V4 is now generally available, with tool calling and long-context support for agent workflows.",`+
					`"site_name":"Example News","site_icon":"https://example.com/favicon.ico","date":"2026-06-01T00:00:00+00:00","authority":3},`+
					`{"rerank_score":0.4,"title":"Release notes","url":"https://example.org/notes","snippet":"Version 4.0.2 fixes a leak.",`+
					`"summary":"","content":"","site_name":"Other","date":"2026-05-01T00:00:00Z","authority":-1},`+
					`{"rerank_score":0.1,"title":"","url":"","content":"no page here"}]}`)
			case mode == "empty":
				io.WriteString(w, `{"results":[]}`)
			case mode == "401", mode == "402", mode == "403", mode == "429", mode == "400", mode == "408", mode == "500":
				words := map[string]string{
					"401": "Authentication Fails, Your api key is invalid", "402": "Insufficient Balance",
					"403": "not allowed", "429": "rate limit reached", "400": "Missing field query",
					"408": "body read timeout", "500": "internal error",
				}[mode]
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(map[string]int{"401": 401, "402": 402, "403": 403, "429": 429, "400": 400, "408": 408, "500": 500}[mode])
				io.WriteString(w, `{"error":{"message":"`+words+`","param":null}}`)
			case mode == "big":
				// a reply cut by the read cap is no JSON any more
				io.WriteString(w, `{"results":[{"title":"x","url":"https://example.com/x","content":"`+strings.Repeat("a", 17<<20)+`"}]}`)
			}
		case r.URL.Path == "/anthropic/v1/messages":
			if !strings.Contains(string(b), `"web_search_20250305"`) {
				http.Error(w, `{"error":{"message":"no search tool"}}`, 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, deepSeekNative)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *deepSeekSite) set(mode, raw string) {
	d.mu.Lock()
	d.mode, d.raw = mode, raw
	d.mu.Unlock()
}

func (d *deepSeekSite) log() (asked, searches []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.asked), slices.Clone(d.searches)
}

// nameDeepSeekService saves a keyed DeepSeek provider on the fake and names
// its service in Settings, as a user who picked it in the Web search section
// has it.
func nameDeepSeekService(t *testing.T, d *deepSeekSite) {
	t.Helper()
	save(t, provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds",
		Chat: d.URL + "/v1", Anthropic: d.URL + "/anthropic", Models: []string{"deepseek-chat"}})
	st := settings.Load()
	st.Searcher = "ds"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
}

// Naming the DeepSeek service posts to its tools/search API with the
// provider's key — count 10, with content, the masked query, magpie's search
// user agent — and no model request is made; the tool result is the numbered
// pages.
func TestDeepSeekServiceSearches(t *testing.T) {
	searchSandbox(t)
	d := newDeepSeekSite(t)
	nameDeepSeekService(t, d)
	call := readMCP(t, mcpPost(lanGuard(New().Handler()), "127.0.0.1:50000", mcpSearchCall, "User-Agent", "Cursor/1.7"))
	if call.Result.IsError || len(call.Result.Content) != 1 ||
		!strings.Contains(call.Result.Content[0].Text, "1. DeepSeek V4 model released — https://example.com/deepseek-v4") ||
		!strings.Contains(call.Result.Content[0].Text, "generally available") {
		t.Fatalf("tools/call: %+v", call)
	}
	asked, searches := d.log()
	for _, p := range asked {
		if p != "/v1/tools/search" {
			t.Fatalf("a model request went out: asked %q", asked)
		}
	}
	if len(searches) != 1 {
		t.Fatalf("searched %q", searches)
	}
	want := "Bearer sk-ds | " + SearchAgent + " | latest go | 10,true | <nil>,<nil>"
	if searches[0] != want {
		t.Fatalf("searched %q, want %q", searches, want)
	}
}

// The reply maps the way the spec says: text from content, then summary,
// then snippet, clipped to 1500 bytes at a rune; date becomes the page's
// age; results without a URL are dropped; DeepSeek's order is kept; and
// site_name, site_icon, authority and rerank_score go nowhere.
func TestDeepSeekServiceMapsResults(t *testing.T) {
	searchSandbox(t)
	d := newDeepSeekSite(t)
	nameDeepSeekService(t, d)
	clipped := strings.Repeat("日", 600) // 1800 bytes: cut at 500 runes
	d.set("", `{"results":[`+
		`{"title":"<b>one</b>","url":"https://example.com/one","content":"content of one","summary":"summary of one","snippet":"snippet of one",`+
		`"site_name":"Example News","site_icon":"https://example.com/favicon.ico","date":"2026-06-01T00:00:00+00:00","authority":3,"rerank_score":2.4},`+
		`{"title":"two","url":"https://example.com/two","content":"","summary":"","snippet":"snippet of two","date":"2026-05-01T00:00:00Z"},`+
		`{"title":"","url":"https://example.com/three","content":"","summary":"","snippet":"snippet of three"},`+
		`{"title":"no url","url":"","content":"dropped"},`+
		`{"title":"clipped","url":"https://example.com/clip","content":"`+clipped+`"}]}`)
	said, hits, err := New().webSearch(context.Background(), "latest go")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 ||
		hits[0].URL != "https://example.com/one" || hits[0].PageAge != "2026-06-01T00:00:00+00:00" ||
		hits[1].URL != "https://example.com/two" || hits[1].PageAge != "2026-05-01T00:00:00Z" ||
		hits[2].URL != "https://example.com/three" || hits[2].PageAge != "" ||
		hits[3].URL != "https://example.com/clip" {
		t.Fatalf("hits %+v", hits)
	}
	for _, want := range []string{
		"1. one — https://example.com/one", "content of one",
		"2. two — https://example.com/two", "snippet of two",
		"3. https://example.com/three", "snippet of three",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("said misses %q", want)
		}
	}
	for _, gone := range []string{"summary of one", "snippet of one", "Example News", "favicon", "2.4", "no url"} {
		if strings.Contains(said, gone) {
			t.Errorf("said carries %q", gone)
		}
	}
	if !strings.Contains(said, clipped[:1500]+"…") || strings.Contains(said, clipped[:1503]) || !utf8.ValidString(said) {
		t.Errorf("the 1500-byte clip missed the rune boundary: %q", said[len(said)-1600:])
	}
}

// The service is a manual-only choice with no model: Settings may name it,
// nothing picks it by itself — not even as the only searcher — and the
// model-based entry is gone.
func TestDeepSeekServiceManualOnly(t *testing.T) {
	searchSandbox(t)
	d := newDeepSeekSite(t)
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds",
		Chat: d.URL + "/v1", Anthropic: d.URL + "/anthropic", Models: []string{"deepseek-chat"}}); err != nil {
		t.Fatal(err)
	}
	// it is the only searcher there would have been
	searchesOn(t, provider.Responses, d.URL)
	searchesOn(t, provider.Anthropic, d.URL)
	if p, _, ok := autoSearcher(); ok || p.ID != "" {
		t.Fatalf("magpie picked %q by itself", p.ID)
	}
	if canSearch() || AutoSearcher() != "" {
		t.Fatal("DeepSeek is searched by itself without being named")
	}
	cs := Searchers()
	if len(cs) != 1 || cs[0].Provider.ID != "ds" || !cs[0].Service || !cs[0].ManualOnly || cs[0].Small != "" || len(cs[0].Models) != 0 {
		t.Fatalf("choices %+v", cs)
	}
	st := settings.Load()
	st.Searcher = "ds"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if !canSearch() || Searcher() != "DeepSeek" || SearcherUnused() != "" {
		t.Fatalf("named: can %v, %q, unused %q", canSearch(), Searcher(), SearcherUnused())
	}
	cp, model, why := chosenSearcher()
	if cp == nil || cp.ID != "ds" || model != "" || why != "" {
		t.Fatalf("named %v %q %q", cp, model, why)
	}
	said, hits, err := New().searchBy(context.Background(), *cp, "", "latest go", "")
	if err != nil || len(hits) != 2 || hits[0].URL != "https://example.com/deepseek-v4" || !strings.Contains(said, "generally available") {
		t.Fatalf("%q %+v %v", said, hits, err)
	}
	if _, searches := d.log(); len(searches) != 1 {
		t.Fatalf("searched %q", searches)
	}
}

// A service that finds nothing, is refused on its key, answers badly or
// reads past the cap falls through to the next source; a refused key rests;
// a 400 surfaces its message; nothing is retried.
func TestDeepSeekSearchFailures(t *testing.T) {
	searchSandbox(t)
	d := newDeepSeekSite(t)
	nameDeepSeekService(t, d)
	apis := newSearchAPIs(t)
	if err := provider.SetSearchAPI(apis.api("tavily", "tvly-k")); err != nil {
		t.Fatal(err)
	}
	s := New()
	p, err := provider.Find("ds")
	if err != nil {
		t.Fatal(err)
	}
	ask := func() error { // the service's own answer
		_, _, err := s.deepSeekSearch(context.Background(), *p, "latest go")
		return err
	}
	fall := func() error { // the whole chain's answer, the search API next
		_, _, err := s.webSearch(context.Background(), "latest go")
		return err
	}
	unrest := func() { s.UnrestAll("ds") }
	tavily := func() int { return len(apis.asked) }

	// empty results: found nothing, the key doesn't rest, the search
	// falls through to the search API, which answers
	d.set("empty", "")
	if err := ask(); err == nil || !strings.Contains(err.Error(), "found nothing") {
		t.Fatalf("empty results: %v", err)
	}
	before := tavily()
	if err := fall(); err != nil {
		t.Fatalf("empty results: %v", err)
	}
	if tavily() != before+1 {
		t.Fatal("empty results did not fall through")
	}
	if _, ok := restOf("ds"); ok {
		t.Fatal("an empty result rested the key")
	}

	// 401/402/403/429: the key rests, the search falls through
	for _, mode := range []string{"401", "402", "403", "429"} {
		unrest()
		d.set(mode, "")
		if err := ask(); err == nil || !strings.Contains(err.Error(), map[string]string{
			"401": "invalid", "402": "Balance", "403": "not allowed", "429": "rate limit",
		}[mode]) {
			t.Fatalf("%s: %v", mode, err)
		}
		before := tavily()
		if err := fall(); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if tavily() != before+1 {
			t.Fatalf("%s did not fall through", mode)
		}
		r, ok := restOf("ds")
		if !ok || r.Status != map[string]int{"401": 401, "402": 402, "403": 403, "429": 429}[mode] {
			t.Fatalf("%s: the key did not rest: %+v", mode, r)
		}
	}

	// 400 surfaces its message and falls through; the key doesn't rest
	unrest()
	d.set("400", "")
	if err := ask(); err == nil || !strings.Contains(err.Error(), "Missing field query") {
		t.Fatalf("400: %v", err)
	}
	before = tavily()
	if err := fall(); err != nil {
		t.Fatalf("400: %v", err)
	}
	if tavily() != before+1 {
		t.Fatal("400 did not fall through")
	}
	if _, ok := restOf("ds"); ok {
		t.Fatal("a 400 rested the key")
	}

	// 408 and 5xx fall through and rest nobody
	for _, mode := range []string{"408", "500"} {
		d.set(mode, "")
		if err := ask(); err == nil || !strings.Contains(err.Error(), map[string]string{
			"408": "timeout", "500": "internal error",
		}[mode]) {
			t.Fatalf("%s: %v", mode, err)
		}
		before := tavily()
		if err := fall(); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if tavily() != before+1 {
			t.Fatalf("%s did not fall through", mode)
		}
		if _, ok := restOf("ds"); ok {
			t.Fatalf("%s rested the key", mode)
		}
	}

	// a reply past the 16 MiB read cap fails as an unexpected reply and
	// falls through
	d.set("big", "")
	if err := ask(); err == nil || !strings.Contains(err.Error(), "unexpected reply") {
		t.Fatalf("big: %v", err)
	}
	before = tavily()
	if err := fall(); err != nil {
		t.Fatalf("big: %v", err)
	}
	if tavily() != before+1 {
		t.Fatal("a cut reply did not fall through")
	}

	// a refused key is passed over for the provider's next one, which
	// answers: the search succeeds on it, and nothing else is asked
	unrest()
	if err := provider.AddKey("ds", "Second", "sk-ds-2", ""); err != nil {
		t.Fatal(err)
	}
	d.set("401", "")
	d.mu.Lock()
	d.refuseKey = "sk-ds"
	d.mu.Unlock()
	before = tavily()
	_, hits, err := s.webSearch(context.Background(), "latest go")
	if err != nil || len(hits) != 2 || hits[0].URL != "https://example.com/deepseek-v4" {
		t.Fatalf("the second key didn't answer: %+v %v", hits, err)
	}
	_, searches := d.log()
	if len(searches) < 2 || !strings.Contains(searches[len(searches)-2], "Bearer sk-ds |") ||
		!strings.Contains(searches[len(searches)-1], "Bearer sk-ds-2") {
		t.Fatalf("searched %q", searches[len(searches)-2:])
	}
	if n := tavily(); n != before {
		t.Fatalf("the search API was asked %d times", n-before)
	}
}

// A successful search records one usage row — kind web_search_api, the
// provider and key used, no tokens, the calling gateway key and agent,
// $0.00075; a failure and an empty result record nothing.
func TestDeepSeekSearchUsageRow(t *testing.T) {
	searchSandbox(t)
	d := newDeepSeekSite(t)
	nameDeepSeekService(t, d)
	keys, secrets := newCaller(t, "Laptop")
	h := lanGuard(New().Handler())
	call := func() mcpReply {
		return readMCP(t, mcpPost(h, "127.0.0.1:50000", mcpSearchCall, "User-Agent", "Cursor/1.7", "Authorization", "Bearer "+secrets[0]))
	}
	if call().Result.IsError {
		t.Fatal("the search failed")
	}
	recs := usage.Load(time.Time{})
	if len(recs) != 1 {
		t.Fatalf("rows %+v", recs)
	}
	rec := recs[0]
	if rec.Kind != "web_search_api" || rec.Provider != "ds" || rec.ProviderKeyID != provider.KeyID("sk-ds") ||
		rec.Input != 0 || rec.Output != 0 || rec.CallerKeyID != keys[0].ID || rec.Agent != "cursor-local" ||
		rec.OwnCost == nil || *rec.OwnCost != 0.00075 {
		t.Fatalf("row %+v", rec)
	}

	// an empty result and a failure record nothing
	d.set("empty", "")
	if !call().Result.IsError {
		t.Fatal("empty results answered")
	}
	d.set("500", "")
	if !call().Result.IsError {
		t.Fatal("a 500 answered")
	}
	if recs = usage.Load(time.Time{}); len(recs) != 1 {
		t.Fatalf("rows after failures %+v", recs)
	}
}

// An Anthropic-protocol request to a DeepSeek model still passes its
// web_search tool through: DeepSeek answers with its own search, the tools
// API isn't asked, and the client sees the tool result as DeepSeek wrote it.
func TestDeepSeekPassThroughUnchanged(t *testing.T) {
	searchSandbox(t)
	d := newDeepSeekSite(t)
	nameDeepSeekService(t, d)
	searchesOn(t, provider.Anthropic, d.URL)
	body := `{"model":"ds/deepseek-chat","max_tokens":3000,"stream":true,"messages":[{"role":"user","content":"今天上海天气"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "web_search_tool_result") || !strings.Contains(out, "https://www.weather.com.cn/weather/101020100.shtml") {
		t.Fatalf("the pass-through search is gone: %s", out)
	}
	asked, searches := d.log()
	if !slices.Equal(asked, []string{"/anthropic/v1/messages"}) || len(searches) != 0 {
		t.Fatalf("asked %q, searched %q", asked, searches)
	}
}

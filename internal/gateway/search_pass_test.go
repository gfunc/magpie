package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/usage"
)

// remoteMagpieSite is a fake of another magpie: /v1/models lists a model it
// searches for itself (web_search "native"), /v1/messages answers a
// passed-through web_search as its vendor does, and /mcp/magpie/web-search
// answers the JSON-RPC tools/call its web_search MCP server serves
// (search_mcp.go's reply shape). Everything it was asked is recorded under
// a lock: the handler runs on many goroutines.
type remoteMagpieSite struct {
	*httptest.Server
	mu    sync.Mutex
	asked []string // "method path | auth | ua | mark | body"
	mode  string   // "", "toolerr", "hang"
	text  string   // the tool answer's text
}

func newRemoteMagpieSite(t *testing.T) *remoteMagpieSite {
	s := &remoteMagpieSite{text: "Go 1.27.1 is out."}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.asked = append(s.asked, strings.Join([]string{
			r.Method + " " + r.URL.Path, r.Header.Get("Authorization"),
			r.Header.Get("User-Agent"), r.Header.Get(SearchingHeader), string(b),
		}, " | "))
		mode, text := s.mode, s.text
		s.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			io.WriteString(w, `{"data":[{"id":"glm-5.3","web_search":"native"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/mcp/magpie/web-search":
			switch mode {
			case "toolerr":
				io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"`+text+`"}],"isError":true}}`)
			case "hang":
				select {
				case <-time.After(30 * time.Second):
				case <-r.Context().Done():
				}
			case "502":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				io.WriteString(w, `{"error":{"message":"bad gateway"}}`)
			case "429":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				io.WriteString(w, `{"error":{"message":"slow down"}}`)
			case "401":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"error":{"message":"the key is no good"}}`)
			default:
				io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"`+text+`"}],"isError":false}}`)
			}
		case r.Method == http.MethodPost:
			// its vendor searches for the model the request names
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"glm-5.3","content":[`+
				`{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"go"}},`+
				`{"type":"web_search_tool_result","tool_use_id":"s1","content":[{"type":"web_search_result","title":"Go","url":"https://go.dev/"}]},`+
				`{"type":"text","text":"Go 1.27.1 is out."}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":4}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *remoteMagpieSite) set(mode, text string) {
	s.mu.Lock()
	s.mode, s.text = mode, text
	s.mu.Unlock()
}

func (s *remoteMagpieSite) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

// saveRemoteMagpie saves a keyed remote magpie on the site.
func saveRemoteMagpie(t *testing.T, id string, s *remoteMagpieSite) provider.Provider {
	t.Helper()
	if err := provider.Save(provider.Provider{ID: id, Name: "Office", Preset: provider.RemoteMagpiePreset,
		Chat: strings.TrimPrefix(s.URL, "http://"), Key: "sk-" + id, KeyName: "Main"}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find(id)
	if err != nil {
		t.Fatal(err)
	}
	return *p
}

// nameRemoteMagpieService saves a keyed remote magpie on the fake and names
// its web search in Settings, as a user who picked it in the Web search
// section has it.
func nameRemoteMagpieService(t *testing.T, s *remoteMagpieSite) {
	t.Helper()
	saveRemoteMagpie(t, "office", s)
	st := settings.Load()
	st.Searcher = "office"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
}

// Naming a keyed remote magpie's service asks the web-search MCP server on
// the remote — one JSON-RPC tools/call for web_search, the masked query,
// the provider's key — and its answer text is the tool result whole: no
// Sources list is added, and no model of the remote is asked.
func TestRemoteMagpieServiceSearches(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	nameRemoteMagpieService(t, s)

	// the settings JSON offers the remote's web search: a service choice,
	// manual-only, with no model
	var choice *SearcherChoice
	cs := Searchers()
	for i := range cs {
		if cs[i].Provider.ID == "office" {
			choice = &cs[i]
		}
	}
	if choice == nil || !choice.Service || !choice.ManualOnly || choice.Small != "" || len(choice.Models) != 0 {
		t.Fatalf("the remote's service entry is missing: %+v", cs)
	}
	if cp, model, why := chosenSearcher(); cp == nil || cp.ID != "office" || model != "" || why != "" {
		t.Fatalf("named %v %q %q", cp, model, why)
	}

	call := readMCP(t, mcpPost(lanGuard(New().Handler()), "127.0.0.1:50000", mcpSearchCall, "User-Agent", "Cursor/1.7"))
	if call.Result.IsError || len(call.Result.Content) != 1 || call.Result.Content[0].Text != "Go 1.27.1 is out." {
		t.Fatalf("tools/call: %+v", call)
	}
	if strings.Contains(call.Result.Content[0].Text, "Sources:") {
		t.Fatal("the pass-through added a Sources list")
	}
	asked := s.log()
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "POST /mcp/magpie/web-search | Bearer sk-office | ") {
		t.Fatalf("the remote magpie was asked %q", asked)
	}
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  struct {
			Name      string `json:"name"`
			Arguments struct {
				Query string `json:"query"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(asked[0][strings.LastIndex(asked[0], "| ")+2:]), &msg); err != nil {
		t.Fatalf("the body wasn't JSON-RPC: %v", err)
	}
	if msg.JSONRPC != "2.0" || string(msg.ID) != "1" || msg.Method != "tools/call" ||
		msg.Params.Name != "web_search" || msg.Params.Arguments.Query != "latest go" {
		t.Fatalf("the tools/call was %s %s %s %s %q", msg.JSONRPC, msg.ID, msg.Method, msg.Params.Name, msg.Params.Arguments.Query)
	}
}

// The pass-through carries the searching mark, so the remote would never
// forward that search to another magpie's searcher: a search another magpie
// sent here (SearchingHeader, a magpie User-Agent) is answered, but its
// named remote service is not asked, while this magpie's own search is.
func TestRemoteMagpieSearchMarksCall(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	nameRemoteMagpieService(t, s)
	h := lanGuard(New().Handler())

	call := readMCP(t, mcpPost(h, "127.0.0.1:50000", mcpSearchCall, "User-Agent", "Cursor/1.7"))
	if call.Result.IsError {
		t.Fatalf("tools/call: %+v", call)
	}
	asked := s.log()
	if len(asked) != 1 {
		t.Fatalf("the remote magpie was asked %q", asked)
	}
	parts := strings.SplitN(asked[0], " | ", 5)
	if parts[3] != "1" || !strings.HasPrefix(parts[2], "magpie/") {
		t.Fatalf("the pass-through carried ua %q, mark %q", parts[2], parts[3])
	}

	// the same magpie, keyed and named, answering a search another magpie
	// sent here: it is not forwarded to the searcher of its own
	other := newRemoteMagpieSite(t)
	saveRemoteMagpie(t, "other", other)
	st := settings.Load()
	st.Searcher = "other"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	m := readMCP(t, mcpPost(h, "127.0.0.1:50000", mcpSearchCall, "User-Agent", "magpie/0.1.999", SearchingHeader, "1"))
	if !m.Result.IsError {
		t.Fatal("a search another magpie made was answered by a searcher of its own")
	}
	if n := len(other.log()); n != 0 {
		t.Fatalf("the marked search was forwarded: %q", other.log())
	}
	if m := readMCP(t, mcpPost(h, "127.0.0.1:50000", mcpSearchCall, "User-Agent", "Cursor/1.7")); m.Result.IsError {
		t.Fatalf("this magpie's own search: %+v", m)
	}
	if n := len(other.log()); n != 1 {
		t.Fatalf("this magpie's own search asked the other magpie %d times", n)
	}
}

// A remote that can't be reached or that doesn't answer in time is a failed
// source and the search falls through to the next one; a remote tool error
// surfaces the text the remote wrote.
func TestRemoteMagpieSearchFailures(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	nameRemoteMagpieService(t, s)
	apis := newSearchAPIs(t)
	if err := provider.SetSearchAPI(apis.api("tavily", "tvly-k")); err != nil {
		t.Fatal(err)
	}
	gw := New()
	office, err := provider.Find("office")
	if err != nil {
		t.Fatal(err)
	}
	ask := func() error { // the service's own answer
		_, _, err := gw.remoteMagpieSearch(context.Background(), *office, "latest go")
		return err
	}
	fall := func() error { // the whole chain's answer, the search API next
		_, _, err := gw.webSearch(context.Background(), "latest go")
		return err
	}
	tavily := func() int { return len(apis.asked) }

	// the remote gone: connection refused, the search falls through
	s.Server.Close()
	if err := ask(); err == nil || !strings.Contains(err.Error(), "Office") {
		t.Fatalf("a dead remote: %v", err)
	}
	before := tavily()
	if err := fall(); err != nil {
		t.Fatalf("a dead remote did not fall through: %v", err)
	}
	if tavily() != before+1 {
		t.Fatal("a dead remote did not fall through")
	}

	// the remote silent: no answer in time
	fresh := newRemoteMagpieSite(t)
	saveRemoteMagpie(t, "office", fresh)
	office, err = provider.Find("office")
	if err != nil {
		t.Fatal(err)
	}
	fresh.set("hang", "")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := gw.remoteMagpieSearch(ctx, *office, "latest go"); err == nil || !strings.Contains(err.Error(), "no answer in time") {
		t.Fatalf("a silent remote: %v", err)
	}

	// a remote tool error surfaces the text the remote wrote, and the
	// search falls through
	fresh.set("toolerr", "its searcher found nothing there")
	if err := ask(); err == nil || !strings.Contains(err.Error(), "its searcher found nothing there") {
		t.Fatalf("a remote tool error: %v", err)
	}
	before = tavily()
	if err := fall(); err != nil {
		t.Fatalf("a remote tool error did not fall through: %v", err)
	}
	if tavily() != before+1 {
		t.Fatal("a remote tool error did not fall through")
	}
}

// A successful pass-through records one usage row — kind web_search_api,
// the remote as the provider, its key, no tokens, the calling gateway key
// and agent, and no cost: the remote's own row carries the spend. A failure
// records nothing.
func TestRemoteMagpieSearchRow(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	nameRemoteMagpieService(t, s)
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
	if rec.Kind != "web_search_api" || rec.Provider != "office" || rec.ProviderKeyID != provider.KeyID("sk-office") ||
		rec.ProviderKeyName != "Main" ||
		rec.Input != 0 || rec.Output != 0 || rec.CallerKeyID != keys[0].ID || rec.Agent != "cursor-local" || rec.OwnCost != nil {
		t.Fatalf("row %+v", rec)
	}

	// a remote tool error records nothing
	s.set("toolerr", "its searcher found nothing there")
	if !call().Result.IsError {
		t.Fatal("a remote tool error answered")
	}
	if recs = usage.Load(time.Time{}); len(recs) != 1 {
		t.Fatalf("rows after the failure %+v", recs)
	}
}

// nameRemoteMagpieFetcher saves a keyed remote magpie on the fake and names
// it in the fetch selector, as a user who picked it under "Fetch pages
// with" has it.
func nameRemoteMagpieFetcher(t *testing.T, s *remoteMagpieSite) {
	t.Helper()
	saveRemoteMagpie(t, "office", s)
	st := settings.Load()
	st.Fetch = "office"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
}

// remoteFetchSaid is a remote web_fetch's answer: the page as the remote's
// own web_fetch words it (the URL:, Title: and the part it read), the JSON
// escapes a tool result carries.
const remoteFetchSaid = `URL: ` + fetchPageURL + `\nTitle: Example Domain\n\n# Example Domain\n\nThis domain is for use in illustrative examples in documents.`

// remoteFetchText is remoteFetchSaid as the tool result's text.
const remoteFetchText = "URL: " + fetchPageURL + "\nTitle: Example Domain\n\n# Example Domain\n\nThis domain is for use in illustrative examples in documents."

// Naming a keyed remote magpie under "Fetch pages with" forwards the url,
// max_chars and offset the tool received to the remote's web_fetch — one
// JSON-RPC tools/call with the provider's key and the searching mark — and
// its answer text comes back as the tool result whole: magpie's own
// downloader reads nothing.
func TestRemoteMagpieFetchForwardsArgs(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	s.set("", remoteFetchSaid)
	nameRemoteMagpieFetcher(t, s)
	own, ownHits := ownSite(t)
	readWithOwn(t, own)

	// the picker offers each keyed remote: Fetchers is what the search
	// state's fetchChoices is built from
	if !slices.ContainsFunc(Fetchers(), func(p provider.Provider) bool { return p.ID == "office" }) {
		t.Fatalf("the remote is not offered: %+v", Fetchers())
	}

	got, isErr := fetchText_(t, lanGuard(New().Handler()), fetchCall(fetchPageURL, `,"max_chars":100,"offset":5`))
	if isErr {
		t.Fatalf("web_fetch: %q", got)
	}
	if got != remoteFetchText {
		t.Fatalf("got\n%s\nwant\n%s", got, remoteFetchText)
	}
	asked := s.log()
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "POST /mcp/magpie/web-search | Bearer sk-office | ") {
		t.Fatalf("the remote magpie was asked %q", asked)
	}
	parts := strings.SplitN(asked[0], " | ", 5)
	if parts[3] != "1" || !strings.HasPrefix(parts[2], "magpie/") {
		t.Fatalf("the pass-through carried ua %q, mark %q", parts[2], parts[3])
	}
	var msg struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string `json:"name"`
			Arguments struct {
				URL      string `json:"url"`
				MaxChars int    `json:"max_chars"`
				Offset   int    `json:"offset"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(parts[4]), &msg); err != nil {
		t.Fatalf("the body wasn't JSON-RPC: %v", err)
	}
	if msg.JSONRPC != "2.0" || msg.ID != 1 || msg.Method != "tools/call" || msg.Params.Name != "web_fetch" ||
		msg.Params.Arguments.URL != fetchPageURL || msg.Params.Arguments.MaxChars != 100 || msg.Params.Arguments.Offset != 5 {
		t.Fatalf("the tools/call was %s %d %s %s %+v", msg.JSONRPC, msg.ID, msg.Method, msg.Params.Name, msg.Params.Arguments)
	}
	if hits := ownHits(); hits != 0 {
		t.Fatal("magpie's own downloader read too")
	}
}

// A remote that can't be reached, or that doesn't answer in time, hands the
// page to magpie's own downloader; a remote tool error surfaces the text
// the remote wrote, and magpie's own downloader is not asked in its place.
func TestRemoteMagpieFetchFallbacks(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	s.set("", remoteFetchSaid)
	nameRemoteMagpieFetcher(t, s)
	own, ownHits := ownSite(t)
	readWithOwn(t, own)
	h := lanGuard(New().Handler())
	page := func() string {
		got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
		if isErr {
			t.Fatalf("web_fetch: %q", got)
		}
		return got
	}

	// the remote answers: its text as-is
	if got := page(); got != remoteFetchText {
		t.Fatalf("the remote's answer: %q", got)
	}

	// the remote gone: a transport failure, magpie's own downloader reads
	s.Server.Close()
	if got := page(); !strings.Contains(got, fetchOwnSaid) {
		t.Fatalf("a dead remote did not fall back: %q", got)
	}

	// the remote silent past the timeout (shrunk here): magpie's own
	// downloader reads
	fresh := newRemoteMagpieSite(t)
	saveRemoteMagpie(t, "office", fresh)
	fresh.set("hang", "")
	was := magpieFetchTimeout
	magpieFetchTimeout = 250 * time.Millisecond
	if got := page(); !strings.Contains(got, fetchOwnSaid) {
		t.Fatalf("a silent remote did not fall back: %q", got)
	}
	magpieFetchTimeout = was

	// a remote tool error surfaces the text the remote wrote, and
	// magpie's own downloader is not asked in its place
	hits := ownHits()
	fresh.set("toolerr", "its fetch found no page there")
	got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if !isErr || !strings.Contains(got, "its fetch found no page there") {
		t.Fatalf("a remote tool error: %v %q", isErr, got)
	}
	if ownHits() != hits {
		t.Fatal("a remote tool error fell back to magpie's own downloader")
	}
}

// A remote answering an HTTP 502 or 429 is failing or busy, not speaking
// about the page: the fetch pass-through falls back on magpie's own
// downloader. An HTTP 401 is the remote's word on the key, and surfaces.
func TestRemoteMagpieFetchHTTPFallbacks(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	s.set("", remoteFetchSaid)
	nameRemoteMagpieFetcher(t, s)
	own, _ := ownSite(t)
	readWithOwn(t, own)
	h := lanGuard(New().Handler())

	for _, mode := range []string{"502", "429"} {
		s.set(mode, "")
		got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
		if isErr || !strings.Contains(got, fetchOwnSaid) {
			t.Fatalf("%s did not fall back: %v %q", mode, isErr, got)
		}
	}
	if n := len(s.log()); n != 2 {
		t.Fatalf("the remote was asked %d times: %q", n, s.log())
	}
	s.set("401", "")
	got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if !isErr || !strings.Contains(got, "the key is no good") {
		t.Fatalf("a 401: %v %q", isErr, got)
	}
}

// A successful pass-through records one usage row — kind web_fetch, the
// remote as the provider with its key, no tokens, the calling gateway key
// and agent, and no cost: the remote's own row carries the spend. A failure
// records nothing.
func TestRemoteMagpieFetchRow(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	s.set("", remoteFetchSaid)
	nameRemoteMagpieFetcher(t, s)
	own, _ := ownSite(t)
	readWithOwn(t, own)
	keys, secrets := newCaller(t, "Laptop")
	h := lanGuard(New().Handler())
	call := func() mcpReply {
		return readMCP(t, mcpPost(h, "127.0.0.1:50000", fetchCall(fetchPageURL, ""),
			"User-Agent", "Cursor/1.7", "Authorization", "Bearer "+secrets[0]))
	}
	if call().Result.IsError {
		t.Fatal("the fetch failed")
	}
	recs := usage.Load(time.Time{})
	if len(recs) != 1 {
		t.Fatalf("rows %+v", recs)
	}
	rec := recs[0]
	if rec.Kind != "web_fetch" || rec.Provider != "office" || rec.ProviderKeyID != provider.KeyID("sk-office") ||
		rec.ProviderKeyName != "Main" ||
		rec.Input != 0 || rec.Output != 0 || rec.CallerKeyID != keys[0].ID || rec.Agent != "cursor-local" || rec.OwnCost != nil {
		t.Fatalf("row %+v", rec)
	}

	// a remote tool error records nothing
	s.set("toolerr", "its fetch found no page there")
	if !call().Result.IsError {
		t.Fatal("a remote tool error answered")
	}
	if recs = usage.Load(time.Time{}); len(recs) != 1 {
		t.Fatalf("rows after the failure %+v", recs)
	}
}

// A fetch provider that is a Remote magpie is warned about and set aside
// like a DeepSeek one when it is gone, off or keyless: FetchUnused says why
// it isn't used, magpie's own downloader reads, and the remote is asked
// nothing.
func TestRemoteMagpieFetchUnused(t *testing.T) {
	fresh(t)
	s := newRemoteMagpieSite(t)
	s.set("", remoteFetchSaid)
	own, _ := ownSite(t)
	readWithOwn(t, own)
	saveRemoteMagpie(t, "office", s)
	if err := provider.Save(provider.Provider{ID: "nk", Name: "Office, no key", Preset: provider.RemoteMagpiePreset,
		Chat: "127.0.0.1:9/v1"}); err != nil {
		t.Fatal(err)
	}
	h := lanGuard(New().Handler())
	read := func() string {
		got, isErr := fetchText_(t, h, fetchCall("http://example.com/page", ""))
		if isErr {
			t.Fatalf("web_fetch: %q", got)
		}
		return got
	}
	name := func(v string) {
		st := settings.Load()
		st.Fetch = v
		if err := settings.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	name("office")
	if got := read(); got != remoteFetchText || len(s.log()) != 1 {
		t.Fatalf("named: %q %q", got, s.log())
	}
	if FetchUnused() != "" {
		t.Fatalf("unused %q", FetchUnused())
	}
	if !slices.ContainsFunc(Fetchers(), func(p provider.Provider) bool { return p.ID == "office" }) ||
		slices.ContainsFunc(Fetchers(), func(p provider.Provider) bool { return p.ID == "nk" }) {
		t.Fatalf("the picker's choices: %+v", Fetchers())
	}
	asked := len(s.log())

	// off: the warning, and magpie's own downloader reads
	if err := provider.Save(provider.Provider{ID: "office", Name: "Office", Preset: provider.RemoteMagpiePreset,
		Chat: strings.TrimPrefix(s.URL, "http://"), Key: "sk-office", Off: true}); err != nil {
		t.Fatal(err)
	}
	if FetchUnused() != SearcherOff {
		t.Fatalf("off: unused %q", FetchUnused())
	}
	if got := read(); !strings.Contains(got, fetchOwnSaid) || len(s.log()) != asked {
		t.Fatalf("off: %q %q", got, s.log())
	}
	if slices.ContainsFunc(Fetchers(), func(p provider.Provider) bool { return p.ID == "office" }) {
		t.Fatalf("an off remote is offered: %+v", Fetchers())
	}

	// keyless
	name("nk")
	if FetchUnused() != SearcherCant {
		t.Fatalf("keyless: unused %q", FetchUnused())
	}
	if got := read(); !strings.Contains(got, fetchOwnSaid) || len(s.log()) != asked {
		t.Fatalf("keyless: %q %q", got, s.log())
	}

	// gone
	if err := provider.Delete("nk"); err != nil {
		t.Fatal(err)
	}
	if FetchUnused() != SearcherGone {
		t.Fatalf("gone: unused %q", FetchUnused())
	}
	if got := read(); !strings.Contains(got, fetchOwnSaid) || len(s.log()) != asked {
		t.Fatalf("gone: %q %q", got, s.log())
	}
}

// A keyed remote magpie's natively-searching models keep working as
// searchers exactly as today, beside its web search service: the list offers
// both, the model searches through a marked request to the remote, and the
// service named bare searches through its MCP server.
func TestRemoteMagpieNativeModelsUnchanged(t *testing.T) {
	searchSandbox(t)
	s := newRemoteMagpieSite(t)
	if err := provider.Save(provider.Provider{ID: "office", Name: "Office", Preset: provider.RemoteMagpiePreset,
		Chat: s.URL, Key: "sk-office"}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("office")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}

	// the list offers its web search beside the models it searches for
	var service, models *SearcherChoice
	for _, c := range Searchers() {
		if c.Provider.ID != "office" {
			continue
		}
		if c.Service {
			service = new(SearcherChoice)
			*service = c
		} else {
			models = new(SearcherChoice)
			*models = c
		}
	}
	if service == nil || !service.ManualOnly || service.Small != "" || len(service.Models) != 0 {
		t.Fatalf("the remote's service entry is missing: %+v", Searchers())
	}
	if models == nil || models.Service || models.Small != "glm-5.3" ||
		!slices.ContainsFunc(models.Models, func(m catalog.Model) bool { return m.ID == "glm-5.3" }) {
		t.Fatalf("the remote's native model is no searcher: %+v", Searchers())
	}

	// named with its model, it searches as it does today: a marked model
	// request to the remote, whose vendor searches
	st := settings.Load()
	st.Searcher = "office/glm-5.3"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if cp, model, why := chosenSearcher(); cp == nil || cp.ID != "office" || model != "glm-5.3" || why != "" {
		t.Fatalf("named %v %q %q", cp, model, why)
	}
	before := len(s.log())
	said, hits, err := New().webSearch(context.Background(), "latest go")
	if err != nil || !strings.Contains(said, "Go 1.27.1") || len(hits) == 0 || hits[0].URL != "https://go.dev/" {
		t.Fatalf("%q %+v %v", said, hits, err)
	}
	asked := s.log()[before:]
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "POST /v1/messages | ") {
		t.Fatalf("the remote magpie was asked %q", asked)
	}
	parts := strings.SplitN(asked[0], " | ", 5)
	if parts[3] != "1" {
		t.Fatalf("the native search went unmarked: %q", asked[0])
	}

	// named bare, its web search answers through the MCP server, and the
	// model is asked no more
	st.Searcher = "office"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if cp, model, why := chosenSearcher(); cp == nil || cp.ID != "office" || model != "" || why != "" {
		t.Fatalf("named %v %q %q", cp, model, why)
	}
	before = len(s.log())
	call := readMCP(t, mcpPost(lanGuard(New().Handler()), "127.0.0.1:50000", mcpSearchCall, "User-Agent", "Cursor/1.7"))
	if call.Result.IsError || call.Result.Content[0].Text != "Go 1.27.1 is out." {
		t.Fatalf("tools/call: %+v", call)
	}
	asked = s.log()[before:]
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "POST /mcp/magpie/web-search | ") {
		t.Fatalf("the remote magpie was asked %q", asked)
	}
}

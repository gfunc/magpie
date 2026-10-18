package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/budget"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/usage"
)

// deepSeekFetchSite is a fake of DeepSeek's tools/fetch API: a POST to
// /v1/tools/fetch of {"url":…} answered with the docs' {"title","content"}
// (api-docs.deepseek.com/api/fetch), or, by mode, a status with an
// OpenAI-shaped error, a reply with no content, one past 5 MiB, a long
// page, a stalled one, or a dropped connection. Everything it was asked is
// recorded under a lock: the handler runs on many goroutines.
type deepSeekFetchSite struct {
	*httptest.Server
	mu        sync.Mutex
	fetches   []string // "auth | ua | url"
	mode      string   // "", a status, "nocontent", "big", "long", "wide", "slow", "abort"
	refuseKey string   // the only key the mode refuses, "" for all of them
}

func newDeepSeekFetchSite(t *testing.T) *deepSeekFetchSite {
	d := &deepSeekFetchSite{}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"deepseek-chat"}]}`)
			return
		}
		if r.URL.Path != "/v1/tools/fetch" {
			http.NotFound(w, r)
			return
		}
		var q map[string]any
		json.Unmarshal(b, &q)
		d.mu.Lock()
		d.fetches = append(d.fetches, strings.Join([]string{
			r.Header.Get("Authorization"), r.Header.Get("User-Agent"), fmt.Sprint(q["url"]),
		}, " | "))
		mode, refuseKey := d.mode, d.refuseKey
		d.mu.Unlock()
		if refuseKey != "" && r.Header.Get("Authorization") != "Bearer "+refuseKey {
			mode = ""
		}
		switch {
		case mode == "slow":
			time.Sleep(2 * time.Second)
			io.WriteString(w, `{"title":"the late page","content":"# too late"}`)
		case mode == "abort":
			panic(http.ErrAbortHandler)
		case mode == "nocontent":
			io.WriteString(w, `{"title":"nothing","content":""}`)
		case mode == "big":
			io.WriteString(w, `{"title":"big","content":"`+strings.Repeat("a", 6<<20)+`"}`)
		case mode == "long":
			io.WriteString(w, `{"title":"long","content":"`+strings.Repeat("ab", 30000)+`"}`)
		case mode == "wide":
			io.WriteString(w, `{"title":"wide","content":"`+strings.Repeat("x", 150000)+`"}`)
		case mode == "":
			io.WriteString(w, `{"title":"Example Domain","content":"# Example Domain\n\nThis domain is for use in illustrative examples in documents."}`)
		default:
			words := map[string]string{
				"400": "invalid request", "402": "Insufficient Balance",
				"403": "Fetch rejected by the security policy", "404": "No usable content found",
				"408": "body read timeout", "413": "body too large", "429": "rate limit exceeded",
				"500": "internal error",
			}[mode]
			status, _ := strconv.Atoi(mode)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"`+words+`","param":null}}`)
		}
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *deepSeekFetchSite) set(mode string) {
	d.mu.Lock()
	d.mode = mode
	d.mu.Unlock()
}

func (d *deepSeekFetchSite) log() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.fetches)
}

// nameDeepSeekFetcher saves a keyed DeepSeek provider on the fake and names
// it in the fetch selector, as a user who picked it under "Fetch pages
// with" has it.
func nameDeepSeekFetcher(t *testing.T, d *deepSeekFetchSite) {
	t.Helper()
	save(t, provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds", KeyName: "Main",
		Chat: d.URL + "/v1", Models: []string{"deepseek-chat"}})
	st := settings.Load()
	st.Fetch = "ds"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
}

// ownSite is the page magpie's own downloader reads, unlike anything
// DeepSeek says, so a test can tell which backend answered. read counts
// the pages it served.
func ownSite(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	served := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		served++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html><html><head><title>Read by magpie</title></head><body><p>magpie's own downloader read this page.</p></body></html>`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return served
	}
}

// fetchPageURL is the page a test's web_fetch asks for: a name that passes
// DeepSeek's checks, as an agent's URL does.
const fetchPageURL = "http://magpie.example/page"

// readWithOwn points magpie's own downloader at the test's server, whatever
// host the URL names — every dial goes to the fake — so the tests can ask
// for the public-looking fetchPageURL.
func readWithOwn(t *testing.T, srv *httptest.Server) {
	t.Helper()
	readWithOwnDial(t, srv.Listener.Addr().String())
}

// readWithOwnDial is readWithOwn with the address magpie's own downloader
// dials named by hand: a dead one, for a downloader that fails.
func readWithOwnDial(t *testing.T, addr string) {
	t.Helper()
	was := webFetchClient
	webFetchClient = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}}
	t.Cleanup(func() { webFetchClient = was })
}

const fetchOwnSaid = "magpie's own downloader read this page."

// Fetch pages with a keyed DeepSeek provider: the checked URL goes to its
// tools/fetch API with the provider key, and the title plus Markdown come
// back under the offset and max_chars the tool always had — 20,000 by
// default, 100,000 at most — with the URL: line naming the URL that was
// asked for. magpie's own downloader is not asked, and the 5 MiB read cap
// and the 30-second timeout hold.
func TestDeepSeekFetchReadsPage(t *testing.T) {
	fresh(t)
	d := newDeepSeekFetchSite(t)
	nameDeepSeekFetcher(t, d)
	own, ownHits := ownSite(t)
	readWithOwn(t, own)
	h := lanGuard(New().Handler())

	got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if isErr {
		t.Fatalf("web_fetch: %q", got)
	}
	asked := d.log()
	if len(asked) != 1 || !strings.Contains(asked[0], "Bearer sk-ds | ") || !strings.Contains(asked[0], fetchPageURL) {
		t.Fatalf("the tools/fetch call: %q", asked)
	}
	want := "URL: " + fetchPageURL + "\nTitle: Example Domain\n\n# Example Domain\n\nThis domain is for use in illustrative examples in documents."
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if hits := ownHits(); hits != 0 {
		t.Fatalf("magpie's own downloader read too: %d", hits)
	}

	// in parts: 10 characters, then on from where it says
	said := "# Example Domain\n\nThis domain is for use in illustrative examples in documents."
	got, _ = fetchText_(t, h, fetchCall(fetchPageURL, `,"max_chars":10`))
	if !strings.Contains(got, "\n\n"+said[:10]+"\n\n["+fmt.Sprint(len(said)-10)+" more characters: call web_fetch with offset=10 to read on]") {
		t.Fatalf("first part: %q", got)
	}
	got, _ = fetchText_(t, h, fetchCall(fetchPageURL, `,"max_chars":10,"offset":10`))
	if !strings.Contains(got, fmt.Sprintf("\n\n%s\n\n[%d more characters: call web_fetch with offset=20 to read on]", said[10:20], len(said)-20)) {
		t.Fatalf("next part: %q", got)
	}

	// the default 20,000, and the 100,000 most
	d.set("long")
	got, _ = fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if !strings.Contains(got, "\n\n"+strings.Repeat("ab", 10000)+"\n\n[40000 more characters: call web_fetch with offset=20000 to read on]") {
		t.Fatalf("the default part: %q", got[len(got)-120:])
	}
	d.set("wide")
	got, _ = fetchText_(t, h, fetchCall(fetchPageURL, `,"max_chars":999999`))
	if !strings.Contains(got, strings.Repeat("x", 100000)+"\n\n[50000 more characters: call web_fetch with offset=100000 to read on]") {
		t.Fatalf("the 100,000 most: %q", got[len(got)-120:])
	}

	// a reply past the 5 MiB read cap is no JSON any more: it fails as an
	// unexpected reply, and magpie's own downloader is not asked in its place
	d.set("big")
	hits := ownHits()
	got, isErr = fetchText_(t, h, fetchCall(fetchPageURL, `,"max_chars":999999`))
	if !isErr || !strings.Contains(got, "unexpected reply") {
		t.Fatalf("a reply past the cap: %v %q", isErr, got)
	}
	if ownHits() != hits {
		t.Fatal("a cut reply fell back to magpie's own downloader")
	}

	// the 30-second timeout holds (shrunk here): the late answer is left,
	// and the page comes from magpie's own downloader
	was := deepSeekFetchTimeout
	deepSeekFetchTimeout = 250 * time.Millisecond
	t.Cleanup(func() { deepSeekFetchTimeout = was })
	d.set("slow")
	got, _ = fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if !strings.Contains(got, fetchOwnSaid) || strings.Contains(got, "the late page") {
		t.Fatalf("the timeout did not hand the page to magpie's own downloader: %q", got)
	}
}

// A URL with credentials, and one at a literal loopback, private or
// link-local address, or localhost, is refused before DeepSeek is asked
// anything: it must never probe an inside target, and must never be handed
// a URL's username or password. No name is resolved here; DeepSeek resolves
// the public host.
func TestDeepSeekFetchRefusesURLs(t *testing.T) {
	fresh(t)
	d := newDeepSeekFetchSite(t)
	nameDeepSeekFetcher(t, d)
	own, ownHits := ownSite(t)
	readWithOwn(t, own)
	h := lanGuard(New().Handler())
	for _, u := range []string{
		"http://127.0.0.1:9/x", "http://[::1]/x", "http://10.1.2.3/x", "http://192.168.1.1/x",
		"http://172.16.5.4/x", "http://169.254.169.254/latest/meta-data/", "http://localhost/x",
		"http://LocalHost:8080/x", "http://user:pass@example.com/x", "http://user@example.com/x",
	} {
		got, isErr := fetchText_(t, h, fetchCall(u, ""))
		if !isErr {
			t.Fatalf("%s answered", u)
		}
		if !strings.Contains(got, "DeepSeek") {
			t.Fatalf("%s: the refusal doesn't say where it stops: %q", u, got)
		}
	}
	if asked := d.log(); len(asked) != 0 {
		t.Fatalf("DeepSeek was asked: %q", asked)
	}
	if hits := ownHits(); hits != 0 {
		t.Fatalf("a refused URL fell back to magpie's own downloader: %d", hits)
	}
	// a public URL goes on as before
	got, isErr := fetchText_(t, h, fetchCall("http://example.com/page", ""))
	if isErr || !strings.Contains(got, "Example Domain") || len(d.log()) != 1 {
		t.Fatalf("a public URL: %v %q %q", isErr, got, d.log())
	}
}

// A key the tools API refuses rests and the next on is asked; with one key
// on, a refusal — 402, 403, 429 — is the keys running out, and the page
// goes to magpie's own downloader the way a transient failure's does. A
// 408, 413 or 5xx, a dropped connection and a timed-out call hand the page
// over too; a 404 and any other 4xx surface DeepSeek's message with no
// fallback; with both backends failing, the error says both.
func TestDeepSeekFetchFallbacks(t *testing.T) {
	fresh(t)
	d := newDeepSeekFetchSite(t)
	nameDeepSeekFetcher(t, d)
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
	for _, mode := range []string{"402", "403", "408", "413", "429", "500"} {
		d.set(mode)
		if got := page(); !strings.Contains(got, fetchOwnSaid) {
			t.Fatalf("%s did not fall back: %q", mode, got)
		}
	}
	d.set("abort")
	if got := page(); !strings.Contains(got, fetchOwnSaid) {
		t.Fatalf("a dropped connection did not fall back: %q", got)
	}
	// the client's timeout (the 30 seconds, shrunk here) falls back too
	was := deepSeekFetchTimeout
	deepSeekFetchTimeout = 250 * time.Millisecond
	d.set("slow")
	if got := page(); !strings.Contains(got, fetchOwnSaid) {
		t.Fatalf("a timed-out call did not fall back: %q", got)
	}
	deepSeekFetchTimeout = was
	d.set("")

	// 404 and the other 4xx that are no refusal surface DeepSeek's
	// message, and magpie's own downloader reads nothing
	hits := ownHits()
	for mode, words := range map[string]string{
		"404": "No usable content", "400": "invalid request",
	} {
		d.set(mode)
		got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
		if !isErr || !strings.Contains(got, words) {
			t.Fatalf("%s: %v %q", mode, isErr, got)
		}
	}
	if ownHits() != hits {
		t.Fatal("a surfaced answer fell back to magpie's own downloader")
	}

	// both failing: the error carries DeepSeek's words and magpie's own
	d.set("500")
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	at := dead.Listener.Addr().String()
	dead.Close()
	readWithOwnDial(t, at)
	got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if !isErr || !strings.Contains(got, "internal error") || !strings.Contains(got, "connection refused") {
		t.Fatalf("both failing: %v %q", isErr, got)
	}
}

// A key the tools API refuses rests, and the provider's next key on is
// asked: the page answers through DeepSeek on it, and magpie's own
// downloader reads nothing.
func TestDeepSeekFetchRotatesKeys(t *testing.T) {
	fresh(t)
	d := newDeepSeekFetchSite(t)
	nameDeepSeekFetcher(t, d)
	if err := provider.AddKey("ds", "Second", "sk-ds-2", ""); err != nil {
		t.Fatal(err)
	}
	own, ownHits := ownSite(t)
	readWithOwn(t, own)
	h := lanGuard(New().Handler())

	d.mu.Lock()
	d.mode, d.refuseKey = "402", "sk-ds"
	d.mu.Unlock()
	got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if isErr || !strings.Contains(got, "Example Domain") {
		t.Fatalf("the second key didn't answer: %v %q", isErr, got)
	}
	asked := d.log()
	if len(asked) != 2 || !strings.Contains(asked[0], "Bearer sk-ds |") || !strings.Contains(asked[1], "Bearer sk-ds-2") {
		t.Fatalf("the keys weren't asked in turn: %q", asked)
	}
	if hits := ownHits(); hits != 0 {
		t.Fatal("magpie's own downloader read too")
	}
	// the refused key rests for the service alone, the way a search's
	// refusal rests it: the key's own model requests are unaffected
	if r, ok := restOf("ds#svc#" + provider.KeyID("sk-ds")); !ok {
		t.Fatalf("the refused key doesn't rest: %+v", r)
	}
	if _, ok := restOf("ds#" + provider.KeyID("sk-ds")); ok {
		t.Fatal("a service refusal rested the key for its models too")
	}
}

// Every key the tools API refuses: the keys ran out, and the page goes to
// magpie's own downloader as any transient failure's does.
func TestDeepSeekFetchKeysOutFallsBack(t *testing.T) {
	fresh(t)
	d := newDeepSeekFetchSite(t)
	nameDeepSeekFetcher(t, d)
	if err := provider.AddKey("ds", "Second", "sk-ds-2", ""); err != nil {
		t.Fatal(err)
	}
	own, _ := ownSite(t)
	readWithOwn(t, own)
	h := lanGuard(New().Handler())

	d.set("402")
	got, isErr := fetchText_(t, h, fetchCall(fetchPageURL, ""))
	if isErr || !strings.Contains(got, fetchOwnSaid) {
		t.Fatalf("the keys ran out and nobody fell back: %v %q", isErr, got)
	}
	if asked := d.log(); len(asked) != 2 {
		t.Fatalf("the keys weren't each asked: %q", asked)
	}
}

// A successful fetch records one usage row — kind web_fetch, the provider
// and key used, no tokens, the calling gateway key and agent, $0.00075;
// a failure and a fallback's success record nothing; and the reservation
// against the calling key runs before either backend, so a key past its
// limit reaches no backend.
func TestDeepSeekFetchUsageRow(t *testing.T) {
	fresh(t)
	budget.Forget()
	pinLimitClock(t)
	d := newDeepSeekFetchSite(t)
	nameDeepSeekFetcher(t, d)
	own, ownHits := ownSite(t)
	readWithOwn(t, own)
	keys, secrets := newCaller(t, "Laptop")
	setLimit(t, keys[0].ID, &access.Limit{Period: "day", Cost: 0.0005})
	h := lanGuard(New().Handler())
	call := func() mcpReply {
		return readMCP(t, mcpPost(h, "127.0.0.1:50000", fetchCall(fetchPageURL, ""),
			"User-Agent", "Cursor/1.7", "Authorization", "Bearer "+secrets[0]))
	}

	// a fallback's success is magpie's own downloader's, not a tools call:
	// it answers, and records nothing
	d.set("500")
	if m := call(); m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, fetchOwnSaid) {
		t.Fatalf("the fallback: %+v", m)
	}
	if recs := usage.Load(time.Time{}); len(recs) != 0 {
		t.Fatalf("a fallback recorded %+v", recs)
	}
	// a surfaced failure records nothing
	d.set("404")
	if m := call(); !m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, "No usable content") {
		t.Fatalf("a 404: %+v", m)
	}
	if recs := usage.Load(time.Time{}); len(recs) != 0 {
		t.Fatalf("a failure recorded %+v", recs)
	}

	// a successful fetch records its row
	d.set("")
	if m := call(); m.Result.IsError {
		t.Fatalf("the fetch: %+v", m)
	}
	recs := usage.Load(time.Time{})
	if len(recs) != 1 {
		t.Fatalf("rows %+v", recs)
	}
	rec := recs[0]
	if rec.Kind != "web_fetch" || rec.Provider != "ds" || rec.ProviderKeyID != provider.KeyID("sk-ds") ||
		rec.ProviderKeyName != "Main" ||
		rec.Input != 0 || rec.Output != 0 || rec.CallerKeyID != keys[0].ID || rec.Agent != "cursor-local" ||
		rec.OwnCost == nil || *rec.OwnCost != 0.00075 {
		t.Fatalf("row %+v", rec)
	}

	// the row's cost counts against the calling key, and the reservation
	// runs before either backend: the next call is refused having asked
	// nobody (one $0.00075 row against a $0.0005 day)
	d.set("500")
	asked, hits := len(d.log()), ownHits()
	if m := call(); !m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, "Laptop") {
		t.Fatalf("a spent key: %+v", m)
	}
	if len(d.log()) != asked || ownHits() != hits {
		t.Fatalf("a spent key reached a backend: %q", d.log())
	}
}

// A fetch provider that is gone, off or keyless is not asked: web_fetch
// keeps reading with magpie's own downloader, and Settings says why the
// named one isn't used (FetchUnused, the warning the page shows).
func TestFetchProviderGoneFallsBack(t *testing.T) {
	fresh(t)
	d := newDeepSeekFetchSite(t)
	own, _ := ownSite(t)
	readWithOwn(t, own)
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds",
		Chat: d.URL + "/v1", Models: []string{"deepseek-chat"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "nk", Name: "DeepSeek, no key", Preset: "deepseek",
		Chat: d.URL + "/v1", Models: []string{"deepseek-chat"}}); err != nil {
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
	asked := 0
	name("ds")
	if got := read(); !strings.Contains(got, "Example Domain") || len(d.log()) != 1 {
		t.Fatalf("named: %q %q", got, d.log())
	}
	asked = len(d.log())
	if FetchUnused() != "" {
		t.Fatalf("unused %q", FetchUnused())
	}

	// off
	off := provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds",
		Chat: d.URL + "/v1", Models: []string{"deepseek-chat"}, Off: true}
	if err := provider.Save(off); err != nil {
		t.Fatal(err)
	}
	if FetchUnused() != SearcherOff {
		t.Fatalf("off: unused %q", FetchUnused())
	}
	if got := read(); !strings.Contains(got, fetchOwnSaid) || len(d.log()) != asked {
		t.Fatalf("off: %q %q", got, d.log())
	}

	// keyless
	name("nk")
	if FetchUnused() != SearcherCant {
		t.Fatalf("keyless: unused %q", FetchUnused())
	}
	if got := read(); !strings.Contains(got, fetchOwnSaid) || len(d.log()) != asked {
		t.Fatalf("keyless: %q %q", got, d.log())
	}

	// gone
	if err := provider.Delete("nk"); err != nil {
		t.Fatal(err)
	}
	if FetchUnused() != SearcherGone {
		t.Fatalf("gone: unused %q", FetchUnused())
	}
	if got := read(); !strings.Contains(got, fetchOwnSaid) || len(d.log()) != asked {
		t.Fatalf("gone: %q %q", got, d.log())
	}
}

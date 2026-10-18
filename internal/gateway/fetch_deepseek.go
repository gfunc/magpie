package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/budget"
	"github.com/yetone/magpie/internal/netproxy"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/usage"
)

// A keyed DeepSeek provider reads a page with its tools API
// (api-docs.deepseek.com/api/fetch): a POST to the provider's Chat base
// URL with /tools/fetch of
//
//	{"url":"…"}
//
// with the key as a Bearer, answered with {"title","content"} — the title
// when there is one, the content Markdown. The same eligibility as the
// tools search (search_deepseek.go): by the provider's preset or its Chat
// host, with a key. The fetch selector (Settings › Web search) names the
// provider; empty is magpie's own downloader. One named that is gone, off
// or keyless is warned about (FetchUnused) and magpie's own downloader
// reads in its place.
//
// A successful call costs the same $0.00075 as a tools search, one row on
// the caller's key. The provider's keys on are asked in turn, the
// rotation's own order: a key the API refuses rests, and the next one is
// asked; every key out, or a transient failure, hands the page to
// fetchMCPRun's other reader — magpie's own downloader — and anything else
// surfaces as it is.

// deepSeekFetchTimeout is how long the tools/fetch call may take, the web
// fetch's own 30 seconds. A var so a test can shrink it.
var deepSeekFetchTimeout = webFetchTimeout

// DeepSeekFetch is where p reads a page, "" for a provider that isn't a
// keyed DeepSeek: its Chat base URL with /tools/fetch.
func DeepSeekFetch(p provider.Provider) string {
	if p.Account != nil || p.Key == "" || p.Chat == "" {
		return ""
	}
	if p.Preset != "deepseek" && provider.HostOf(p.Chat) != "api.deepseek.com" {
		return ""
	}
	return strings.TrimSuffix(p.Chat, "/") + "/tools/fetch"
}

// fetchService is where p reads a page as the fetch backend, "" for a
// provider that doesn't read here: a keyed DeepSeek's tools fetch, or a
// keyed Remote magpie's web-search MCP server, whose own web_fetch
// answers. The fetch picker's choices, the warning and the chosen backend
// all go by it.
func fetchService(p provider.Provider) string {
	if u := DeepSeekFetch(p); u != "" {
		return u
	}
	return remoteMagpieService(p)
}

// Fetchers are the providers the "Fetch pages with" picker may name: the
// keyed DeepSeek providers and keyed Remote magpies that are on.
func Fetchers() []provider.Provider {
	var out []provider.Provider
	for _, p := range provider.All() {
		if p.On() && fetchService(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// fetchNamed is the fetch provider Settings names, and why it isn't used —
// one of the Searcher* reasons, "" when it is or none is named: gone, off,
// or with no page reader (a keyed DeepSeek or a keyed Remote magpie is
// what reads here — not one of those, or one without a key, can't).
func fetchNamed() (*provider.Provider, string) {
	v := strings.TrimSpace(settings.Load().Fetch)
	if v == "" {
		return nil, ""
	}
	i := slices.IndexFunc(provider.All(), func(p provider.Provider) bool { return p.ID == v })
	if i < 0 {
		return nil, SearcherGone
	}
	p := provider.All()[i]
	if fetchService(p) == "" {
		return &p, SearcherCant
	}
	if !p.On() {
		return &p, SearcherOff
	}
	return &p, ""
}

// FetchUnused is why the fetch provider Settings names isn't used (one of
// the Searcher* reasons), "" when it is or none is named. See fetchNamed.
func FetchUnused() string {
	_, why := fetchNamed()
	return why
}

// chosenFetcher is the fetch provider Settings names while it can be used,
// nil otherwise: none named, gone, off, or not a keyed DeepSeek or Remote
// magpie. Whatever the reason, magpie's own downloader reads in its place.
func chosenFetcher() *provider.Provider {
	p, why := fetchNamed()
	if p == nil || why != "" {
		return nil
	}
	return p
}

// fetchDeepSeekURL checks u before it is sent to DeepSeek: a URL with a
// username or password would hand the credentials over, and one at a
// literal loopback, private or link-local address, or localhost, would
// have DeepSeek probe an inside target. No name is resolved here —
// DeepSeek resolves the public host.
func fetchDeepSeekURL(u *url.URL) error {
	host := strings.ToLower(u.Hostname())
	if u.User != nil {
		return errors.New("a URL with a username or password is not sent to DeepSeek")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("a URL at localhost is not sent to DeepSeek")
	}
	if addr, err := netip.ParseAddr(host); err == nil &&
		(addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()) {
		return errors.New("a URL at a loopback, private or link-local address is not sent to DeepSeek")
	}
	return nil
}

// fetchTransient is a keyed fetch's failure another reader — magpie's own
// downloader here — may answer: a DeepSeek 408, 413 or 5xx, every key of a
// fetch provider refused (deepSeekFetch wraps the refusals), a Remote
// magpie that can't be reached, doesn't answer in time, or answers an HTTP
// 429 or 5xx (magpieMCPCall's wraps), a transport error or the client's
// timeout. Anything else surfaces as it is.
type fetchTransient struct{ err error }

func (e fetchTransient) Error() string { return e.err.Error() }
func (e fetchTransient) Unwrap() error { return e.err }

// deepSeekFetch asks p's tools/fetch for the page at raw, over the
// provider's keys in turn like its search: a key the API refuses rests,
// and the next one on is asked. A transient failure comes back as it is
// for fetchByDeepSeek to fall back on magpie's own downloader, and so does
// every key refused — the keys ran out — while anything else surfaces.
func (s *Server) deepSeekFetch(ctx context.Context, p provider.Provider, raw string) (title, text string, err error) {
	keys := p.KeysOn()
	if len(keys) == 0 {
		return "", "", fmt.Errorf("%s: no key to fetch with", p.Name)
	}
	// a key resting from an earlier refusal is asked last, when it is all
	// the provider has on
	var ready, resting []provider.KeyAccount
	for _, k := range keys {
		if _, held := restOf(serviceRest(p, k.Key, len(keys))); held {
			resting = append(resting, k)
		} else {
			ready = append(ready, k)
		}
	}
	var errs []error
	asked := 0
	for i, k := range append(ready, resting...) {
		one := p.WithKey(k)
		at := DeepSeekFetch(one)
		if at == "" {
			// a key made for another protocol: its provider has no Chat
			// endpoint to fetch on
			continue
		}
		asked++
		gotTitle, gotText, err := s.deepSeekFetchKey(ctx, one, at, raw, k.Key)
		if err == nil {
			return gotTitle, gotText, nil
		}
		var r searchRefused
		if len(keys) > 1 {
			err = fmt.Errorf("%s: %w", keyLabel(i, k.Key), err)
		}
		if !errors.As(err, &r) {
			return "", "", err
		}
		errs = append(errs, err)
	}
	if asked == 0 {
		return "", "", fmt.Errorf("%s: no key of the ones on reads here", p.Name)
	}
	// the keys ran out: magpie's own downloader reads in their place, the
	// way a transient failure's does
	return "", "", fetchTransient{errors.Join(errs...)}
}

// deepSeekFetchKey asks the tools API with one key, and records the call's
// usage row when it answers with a page.
func (s *Server) deepSeekFetchKey(ctx context.Context, p provider.Provider, at, raw, key string) (string, string, error) {
	ctx, cancel := context.WithTimeout(s.metered(p.Via(ctx), p, ""), deepSeekFetchTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"url": raw})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, at, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", SearchAgent)
	r.Header.Set("Authorization", "Bearer "+key)
	res, err := s.client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", fetchTransient{fmt.Errorf("%s: no answer in time", p.Name)}
		}
		return "", "", fetchTransient{fmt.Errorf("%s: %w", p.Name, err)}
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, webFetchRead))
	// the error words live at the front of a reply
	head := b[:min(len(b), 1<<20)]
	if res.StatusCode >= 300 {
		// an API that says the key back in its error doesn't get it into
		// magpie's logs or the model's turn
		msg := strings.ReplaceAll(provider.APIError(head, res.Status), key, provider.MaskSearchKey(key))
		if keyRefusal(res.StatusCode) {
			// the key rests as a refusal of its would, and the next is asked
			s.restSearchKey(p, res.StatusCode, res.Header, b)
			return "", "", searchRefused{res.StatusCode, msg, res.Header.Get("Retry-After")}
		}
		if res.StatusCode == 408 || res.StatusCode == 413 || res.StatusCode >= 500 {
			return "", "", fetchTransient{errors.New(p.Name + ": " + msg)}
		}
		return "", "", errors.New(p.Name + ": " + msg)
	}
	var out struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", "", fmt.Errorf("%s: unexpected reply: %s", p.Name, provider.APIError(head, res.Status))
	}
	if strings.TrimSpace(out.Content) == "" {
		return "", "", fmt.Errorf("%s: no page content in its answer", p.Name)
	}
	s.recordDeepSeekFetch(ctx, p, key)
	return searchPlain(out.Title), out.Content, nil
}

// recordDeepSeekFetch records the tools call a fetch made: one row for the
// caller's key, the provider and the key used, no tokens, the call's
// price. A failure never gets here.
func (s *Server) recordDeepSeekFetch(ctx context.Context, p provider.Provider, key string) {
	cost := deepSeekSearchCost
	rec := usage.Record{Time: time.Now(), Kind: "web_fetch", Provider: p.ID, Host: p.Where(),
		ProviderKeyID: provider.KeyID(key), ProviderKeyName: p.KeyName, Status: http.StatusOK, OwnCost: &cost}
	if f := searchFor(ctx); f != nil {
		rec.Agent = f.Agent
	}
	if who := access.Caller(ctx); who.KeyID != "" {
		rec.CallerKeyID, rec.CallerKeyName = who.KeyID, who.KeyName
	}
	budget.Append(rec)
}

// fetchByDeepSeek reads the page through DeepSeek's tools/fetch, with the
// tool's own offset and limit on its Markdown. A transient failure — a
// 408, 413 or 5xx, a transport error, the client's timeout, every key
// refused — hands the page to magpie's own downloader; both failing says
// both. Anything else surfaces: a 404 or 400 is DeepSeek's word on the
// page or the request.
func (s *Server) fetchByDeepSeek(ctx context.Context, p provider.Provider, u *url.URL, raw string, offset, limit int) (string, error) {
	if err := fetchDeepSeekURL(u); err != nil {
		return "", searchMCPError(p.Name + ": " + err.Error())
	}
	title, text, err := s.deepSeekFetch(ctx, p, raw)
	if err == nil {
		// DeepSeek doesn't say where its own redirects ended: the URL:
		// line names the URL that was asked for
		return fetchPart(u.Redacted(), title, text, offset, limit), nil
	}
	var transient fetchTransient
	if !errors.As(err, &transient) {
		return "", searchMCPError(err.Error())
	}
	said, ferr := s.fetchOwn(ctx, u, offset, limit)
	if ferr != nil {
		return "", searchMCPError("couldn't fetch " + u.Redacted() + ": " + err.Error() + "; " + ferr.Error())
	}
	return said, nil
}

// fetchOwn reads the page with magpie's own downloader, as web_fetch
// always has, on its own 30 seconds.
func (s *Server) fetchOwn(ctx context.Context, u *url.URL, offset, limit int) (string, error) {
	ctx, cancel := context.WithTimeout(netproxy.With(ctx, ""), webFetchTimeout)
	defer cancel()
	title, text, final, err := fetchPage(ctx, u)
	if err != nil {
		return "", searchMCPError("couldn't fetch " + u.Redacted() + ": " + err.Error())
	}
	return fetchPart(final, title, text, offset, limit), nil
}

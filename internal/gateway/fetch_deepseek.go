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
// the caller's key. The provider's first key on is asked (the rotation's
// own order); a failure comes back for fetchMCPRun to fall back on
// magpie's own downloader or surface as it is.

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

// Fetchers are the providers the "Fetch pages with" picker may name: the
// keyed DeepSeek providers that are on.
func Fetchers() []provider.Provider {
	var out []provider.Provider
	for _, p := range provider.All() {
		if p.On() && DeepSeekFetch(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// FetchUnused is why the fetch provider Settings names isn't used (one of
// the Searcher* reasons), "" when it is or none is named: gone, turned
// off, or without a key (a keyed DeepSeek is what reads here).
func FetchUnused() string {
	v := strings.TrimSpace(settings.Load().Fetch)
	if v == "" {
		return ""
	}
	i := slices.IndexFunc(provider.All(), func(p provider.Provider) bool { return p.ID == v })
	if i < 0 {
		return SearcherGone
	}
	p := provider.All()[i]
	if DeepSeekFetch(p) == "" {
		return SearcherCant
	}
	if !p.On() {
		return SearcherOff
	}
	return ""
}

// chosenFetcher is the fetch provider Settings names while it can be used,
// nil otherwise: none named, gone, off, or not a keyed DeepSeek. Whatever
// the reason, magpie's own downloader reads in its place.
func chosenFetcher() *provider.Provider {
	if FetchUnused() != "" {
		return nil
	}
	v := strings.TrimSpace(settings.Load().Fetch)
	i := slices.IndexFunc(provider.All(), func(p provider.Provider) bool { return p.ID == v })
	if i < 0 {
		return nil
	}
	p := provider.All()[i]
	return &p
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

// fetchTransient is a DeepSeek fetch failure another reader — magpie's own
// downloader here — may answer: 408, 413, 429, a 5xx, a transport error or
// the client's timeout. Anything else surfaces as it is.
type fetchTransient struct{ err error }

func (e fetchTransient) Error() string { return e.err.Error() }
func (e fetchTransient) Unwrap() error { return e.err }

// deepSeekFetch asks p's tools/fetch for the page at raw, with the
// provider's first key on.
func (s *Server) deepSeekFetch(ctx context.Context, p provider.Provider, raw string) (title, text string, err error) {
	keys := p.KeysOn()
	if len(keys) == 0 {
		return "", "", fmt.Errorf("%s: no key to fetch with", p.Name)
	}
	return s.deepSeekFetchKey(ctx, p.WithKey(keys[0]), DeepSeekFetch(p), raw, keys[0].Key)
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
		if res.StatusCode == 408 || res.StatusCode == 413 || res.StatusCode == 429 || res.StatusCode >= 500 {
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
		ProviderKeyID: provider.KeyID(key), Status: http.StatusOK, OwnCost: &cost}
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
// 408, 413, 429 or 5xx, a transport error, the client's timeout — hands
// the page to magpie's own downloader; both failing says both. Anything
// else surfaces: a 402, 403 or 404 is DeepSeek's word on the account, the
// policy or the page.
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

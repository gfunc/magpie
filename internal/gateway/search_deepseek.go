package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/provider"
)

// A keyed DeepSeek provider searches the web with its tools API directly
// (api-docs.deepseek.com/api/search): a POST to the provider's Chat base
// URL with /tools/search of
//
//	{"query":"…","count":10,"need_content":true}
//
// with the key as a Bearer, answered with {"results":[{title,url,snippet,
// summary,content,date,…}]}. No model is asked and no model tokens are
// spent; a successful call costs $0.00075, recorded on the caller's key.
// Like a Kimi Code plan's search service (search_kimi.go) it is a
// manual-only choice with no model: Settings may name it, nothing picks it
// by itself. A provider is on it by its preset or its Chat host — one
// added by hand, pointed at DeepSeek, is recognized too — with a key.

// deepSeekSearchCount is how many results a search asks for, the API's
// default kept explicit.
const deepSeekSearchCount = 10

// deepSeekSearchCost is what one successful tools call costs, per request
// and not per token (api-docs.deepseek.com/quick_start/pricing).
const deepSeekSearchCost = 0.00075

// deepSeekSearchRead is how much of a reply the tools caller reads: ten
// results with their content can be past the search APIs' 4 MiB. A reply
// cut by it is no JSON any more, and fails as an unexpected reply.
const deepSeekSearchRead = 16 << 20

// DeepSeekSearch is where p's key searches the web, "" for a provider that
// isn't a keyed DeepSeek: its Chat base URL with /tools/search.
func DeepSeekSearch(p provider.Provider) string {
	if p.Account != nil || p.Key == "" || p.Chat == "" {
		return ""
	}
	if p.Preset != "deepseek" && provider.HostOf(p.Chat) != "api.deepseek.com" {
		return ""
	}
	return strings.TrimSuffix(p.Chat, "/") + "/tools/search"
}

// deepSeekSearch asks p's tools API, over the provider's keys in turn: a
// key the API refuses rests, and the next one on is asked; anything else
// comes back as the failure it is, and no key is asked twice.
func (s *Server) deepSeekSearch(ctx context.Context, p provider.Provider, query string) (string, []Hit, error) {
	q, err := maskedQuery(query, &p)
	if err != nil {
		return "", nil, err
	}
	keys := p.KeysOn()
	if len(keys) == 0 {
		return "", nil, fmt.Errorf("%s: no key to search with", p.Name)
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
		at := DeepSeekSearch(one)
		if at == "" {
			// a key made for another protocol: its provider has no Chat
			// endpoint to search on
			continue
		}
		asked++
		said, hits, err := s.deepSeekSearchKey(ctx, one, at, q, k.Key)
		if err == nil {
			return said, hits, nil
		}
		var r searchRefused
		if len(keys) > 1 {
			err = fmt.Errorf("%s: %w", keyLabel(i, k.Key), err)
		}
		if !errors.As(err, &r) {
			return "", nil, err
		}
		errs = append(errs, err)
	}
	// every key was made for another protocol: no key of the ones on has a
	// Chat endpoint to search on
	if asked == 0 {
		return "", nil, fmt.Errorf("%s: no key of the ones on searches here", p.Name)
	}
	return "", nil, errors.Join(errs...)
}

// deepSeekSearchKey asks the tools API with one key, and records the call's
// usage row when it answers with pages.
func (s *Server) deepSeekSearchKey(ctx context.Context, p provider.Provider, at, q, key string) (string, []Hit, error) {
	ctx, cancel := context.WithTimeout(s.metered(p.Via(ctx), p, ""), searchTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"query": q, "count": deepSeekSearchCount, "need_content": true})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, at, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", SearchAgent)
	r.Header.Set("Authorization", "Bearer "+key)
	res, err := s.client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return "", nil, fmt.Errorf("%s: no answer in time", p.Name)
		}
		return "", nil, fmt.Errorf("%s: %w", p.Name, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, deepSeekSearchRead))
	// the error words live at the front of a reply; a full one can be 16 MiB
	head := b[:min(len(b), 1<<20)]
	if res.StatusCode >= 300 {
		// an API that says the key back in its error doesn't get it into
		// magpie's logs or the model's turn
		msg := strings.ReplaceAll(provider.APIError(head, res.Status), key, provider.MaskSearchKey(key))
		if keyRefusal(res.StatusCode) {
			// the key rests as a refusal of its would, and the next is asked
			s.restSearchKey(p, res.StatusCode, res.Header, b)
			return "", nil, searchRefused{res.StatusCode, msg, res.Header.Get("Retry-After")}
		}
		return "", nil, errors.New(p.Name + ": " + msg)
	}
	var out struct {
		Results *[]struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Summary string `json:"summary"`
			Content string `json:"content"`
			Date    string `json:"date"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Results == nil {
		return "", nil, fmt.Errorf("%s: unexpected reply: %s", p.Name, provider.APIError(head, res.Status))
	}
	var pages []foundPage
	for _, x := range *out.Results {
		if x.URL == "" {
			continue
		}
		title := searchPlain(x.Title)
		if title == "" {
			title = x.URL
		}
		text := x.Content
		if text == "" {
			text = x.Summary
		}
		if text == "" {
			text = x.Snippet
		}
		pages = append(pages, foundPage{Title: title, URL: x.URL, Text: clipText(searchPlain(text), pageText), Age: x.Date})
		if len(pages) == deepSeekSearchCount {
			break
		}
	}
	if len(pages) == 0 {
		return "", nil, fmt.Errorf("%s %w", p.Name, errFoundNothing)
	}
	said, hits := pagesSaid(pages)
	cost := deepSeekSearchCost
	s.recordServiceSearch(ctx, p, key, "web_search_api", &cost)
	return said, hits, nil
}

// serviceRest is what a keyed service's refusal rests under: the
// provider's id scoped to its tools service, per key when several are on —
// never the bare id a model's own request rests by, so a page DeepSeek
// won't read, or the tools API's own limit, benches the key for the
// service alone. A key a model's request finds refused rests by that
// request's own path.
func serviceRest(p provider.Provider, key string, keys int) string {
	rest := p.ID + "#svc"
	if keys > 1 {
		rest += "#" + provider.KeyID(key)
	}
	return rest
}

// restSearchKey rests the key p was asked with for the service alone
// (serviceRest): the service's other keys are asked first until the rest
// is up, and the key's own model requests are unaffected.
func (s *Server) restSearchKey(p provider.Provider, status int, header http.Header, body []byte) {
	s.restAfterMarked(candidate{p: p, rest: serviceRest(p, p.Key, len(p.KeysOn()))}, status, header, body, false)
}

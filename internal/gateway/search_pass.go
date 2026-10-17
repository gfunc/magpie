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

// A keyed Remote magpie searches the web through the web-search MCP server
// of its own (SearchMCPPath on the remote): one JSON-RPC tools/call for
// web_search with the query, the provider's key as the gateway key. The
// remote's whole chain — its searcher, its Search APIs, its services —
// answers, and its answer text is the local search result with no pages of
// its own: no Sources list is added. Every pass-through carries the
// searching mark (SearchingHeader) with magpie's User-Agent, so the remote
// runs no searcher of its own for it — and a search another magpie sent
// here (searchFromMagpie) is never forwarded to a keyed remote service,
// so two magpies that name each other's services cannot loop. Like a Kimi
// Code plan's and DeepSeek's keyed services (search_service.go) the
// remote's web search is a manual-only choice with no model, beside the
// remote's natively-searching models, which keep working as searchers as
// they do today. A transport failure or a timeout is a failed source like
// any searcher's; a remote tool error surfaces the text the remote wrote.
// A successful call records a zero-cost row: the spend is the remote's,
// on its own usage.

// magpieSearchRead is how much of a reply the pass-through reads: the
// remote's answer is one tool result, and 4 MiB is past any of them. A
// reply cut by it is no JSON any more, and fails as an unexpected reply.
const magpieSearchRead = 4 << 20

// remoteMagpieService is where p's key searches through its own web-search
// MCP server, "" for a provider that isn't a keyed remote magpie: the
// remote's base — its Chat base carries /v1 — with SearchMCPPath.
func remoteMagpieService(p provider.Provider) string {
	if p.Account != nil || !p.IsRemoteMagpie() || p.Key == "" || p.Chat == "" {
		return ""
	}
	return strings.TrimSuffix(strings.TrimRight(p.Chat, "/"), "/v1") + SearchMCPPath
}

// remoteMagpieSearch asks p's web_search on the remote: its answer text,
// with no pages — the remote's chain wrote it. The mark rides the request
// whoever the search is for, and a search another magpie sent here is not
// forwarded again.
func (s *Server) remoteMagpieSearch(ctx context.Context, p provider.Provider, query string) (string, []Hit, error) {
	if searchFromMagpie(ctx) {
		return "", nil, fmt.Errorf("%s: a search another magpie made is not forwarded to a searcher of its own", p.Name)
	}
	q, err := maskedQuery(query, &p)
	if err != nil {
		return "", nil, err
	}
	text, err := s.magpieMCPCall(ctx, p, remoteMagpieService(p), p.Key, searchMCPTool, map[string]any{"query": q})
	if err != nil {
		return "", nil, err
	}
	if text == "" {
		return "", nil, fmt.Errorf("%s %w", p.Name, errFoundNothing)
	}
	s.recordServiceSearch(ctx, p, p.Key, nil)
	return text, nil, nil
}

// magpieMCPCall asks the web-search MCP server at at — p's, with its key —
// one JSON-RPC tools/call for tool with args, and says the text its result
// carried. A remote tool error is the error, carrying the remote's text; a
// transport failure, an HTTP error or a JSON-RPC error is the error alone,
// which the caller treats as any failure of its own. The fetch pass-through
// asks it for web_fetch the same way.
func (s *Server) magpieMCPCall(ctx context.Context, p provider.Provider, at, key, tool string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(s.metered(p.Via(ctx), p, ""), searchTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, at, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", magpieUA())
	r.Header.Set("Authorization", "Bearer "+key)
	// the remote runs no searcher of its own for a marked search, so two
	// magpies that name each other's services cannot loop (search_remote.go)
	r.Header.Set(SearchingHeader, "1")
	res, err := s.client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: no answer in time", p.Name)
		}
		return "", fmt.Errorf("%s: %w", p.Name, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, magpieSearchRead))
	head := b[:min(len(b), 1<<20)]
	if res.StatusCode >= 300 {
		// an error that says the key back doesn't get it into magpie's logs
		msg := strings.ReplaceAll(provider.APIError(head, res.Status), key, provider.MaskSearchKey(key))
		return "", errors.New(p.Name + ": " + msg)
	}
	var out struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("%s: unexpected reply: %s", p.Name, provider.APIError(head, res.Status))
	}
	if out.Error != nil {
		return "", fmt.Errorf("%s: %s", p.Name, out.Error.Message)
	}
	text := ""
	for _, c := range out.Result.Content {
		if c.Type == "text" {
			text = c.Text
			break
		}
	}
	if out.Result.IsError {
		if text == "" {
			return "", fmt.Errorf("%s: the tool failed there", p.Name)
		}
		return "", errors.New(text)
	}
	return text, nil
}

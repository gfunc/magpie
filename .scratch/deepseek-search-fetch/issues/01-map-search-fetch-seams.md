# Map magpie's search and web_fetch seams

Status: resolved
Type: research

## Question

Where does magpie decide that a provider searches natively, where does it run its own searcher, and how does `web_fetch` work — enough to place DeepSeek's `/v1/tools/search` and `/v1/tools/fetch` in the right seams?

## Answer

Recorded in [magpie's search and web_fetch seams](../research/search-fetch-seams.md).

The short version:

- **Pass-through** is protocol and host based: `searchesItself` and `searchHosts` (`internal/gateway/search.go:100-116,202-207`) already list `api.deepseek.com` for Anthropic and Responses. `Provider.Searches` is a custom-provider/plugin flag; no preset sets it.
- **Turn-aware searcher** is `ownSearcher` (`internal/gateway/search_kimi.go:73-79`), used today by Kimi Code, WorkBuddy, and Google. It can see the turn's provider and the key chosen for the turn (`searchOnKey`, `gateway.go:5582`).
- **Global searcher** is `searcher()`/`autoSearcher()` (`internal/gateway/search.go:223-355`); it ignores the turn's model. `Settings.Searcher` names a provider/model explicitly.
- **Search-API vendors** are Tavily/Brave/Exa/Firecrawl/You/SearXNG (`internal/provider/search_api.go:56-63`, adapter at `internal/gateway/search_api.go:204-316`).
- `searchWith` (`search.go:784-857`) re-enters `s.serve` with only `provider/model`, so a pinned key does not survive; DeepSeek search should call the tools API directly instead.
- `web_fetch` (`internal/gateway/search_fetch.go:66-95`) is provider- and model-agnostic, with no backend abstraction; `webFetchClient = publicClient` is the only seam.
- Search-API and Kimi/WorkBuddy search calls record no usage or cost today. Searcher-model calls do.

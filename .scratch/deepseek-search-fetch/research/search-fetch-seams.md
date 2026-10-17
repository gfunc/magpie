# Magpie's search and web_fetch seams

Read-only exploration of `/home/jason/projects/magpie` at `main` (fork tip `4fc49822`), 2026-10-17.

## Provider-native search selection

`Provider.Searches` (`internal/provider/provider.go:215-220`) says the vendor answers a web search tool offered on its Anthropic or Responses API by itself. It is set from:

- CLI `provider ... search=yes|no` — `internal/cli/providers_cli.go:1046-1063`, shown at `:785-789`.
- GUI custom-provider editor checkbox — saved through `internal/gui/providers.go:1320`, rendered at `internal/gui/assets/app.js:9212-9216` (guarded by `if (custom)`, so presets get no toggle).
- Plugin config hook — `internal/provider/plugins.go:410-412`, fed by `internal/plugin/providers.go:132-136`.
- Import/rename merge — `internal/provider/provider.go:963`.

No built-in preset sets it. The DeepSeek preset (`internal/provider/presets.go:142-144`) has only Chat, Responses, and Anthropic URLs.

`searchesItself` (`internal/gateway/search.go:100-116`):

```go
func searchesItself(p provider.Provider, proto provider.Protocol) bool {
	saidTo := p.Searches && (proto == provider.Anthropic || proto == provider.Responses) && p.Base(proto) != ""
	if p.Account != nil {
		return (p.Account.Agent == "codex" || p.ID == "grok" || p.PluginProvider() == "grok") && proto == provider.Responses ||
			p.IsPlugin() && saidTo
	}
	if saidTo {
		return true
	}
	return slices.Contains(searchHosts[proto], provider.HostOf(p.Base(proto)))
}
```

`searchHosts` (`search.go:202-207`):

```go
Responses: {"api.openai.com", "api.x.ai", "api.deepseek.com", "open.bigmodel.cn", "api.z.ai"},
Anthropic: {"api.anthropic.com", "api.deepseek.com"},
Chat:      {"openrouter.ai"},
Gemini:    {"generativelanguage.googleapis.com"},
```

So native searchers are discovered by protocol and host, not just the flag. DeepSeek is already treated as native on Anthropic and Responses.

Per-request call sites:

- `forwardTranslated`: `if want := web && searchesFor(p, to, model, req); want != req.WebSearch { ... }` — `gateway.go:4864-4866`.
- `translate`: loops `s.usable(p, model)` for a protocol that searches natively; if none and `canSearchFor(p)`, calls `searchReply` — `gateway.go:5564-5583`.
- Parsers turn client search tools into `Request.WebSearch`: `anthropic.go:198-203`, `chat.go:130-135`, `responses.go:404-406` and `461-465`, `gemini.go:339`.
- Builders re-emit the vendor tool on the native path: `web_search_20250305` (`anthropic.go:984-986`), `{"type":"web_search"}` (`responses.go:1211-1217`), OpenRouter's `plugins:[{"id":"web"}]` (`chat.go:528-530`).

## Searcher choice

`searcher()` (`search.go:223-355`):

```go
func searcher() (provider.Provider, string, bool) {
	if settings.Load().Searcher == "off" { return ..., false }
	if p, m, why := chosenSearcher(); why == "" && p != nil { return *p, m, true }
	return autoSearcher()
}
```

- `chosenSearcher` (`:324-355`) parses Settings' `"<provider>"` or `"<provider>/<model>"`; falls back to the provider's small model.
- `searchRank` (`:238-259`) orders Claude account, Codex account, Anthropic-native, Responses-native, Chat-native, Google account/remote magpie. It deliberately clears `p.Searches` first, so relays/plugins said to search are manual-only.
- `autoSearcherReady` (`:293-309`) walks `provider.All()`; `Searchers()` (`:360-384`) returns the GUI/CLI choices.
- `modelSearch` calls `searcher()` with no model argument (`search.go:598-599`): the global searcher is not turn-aware. The turn-aware path is `ownSearcher` (`search_kimi.go:73-79`) for Kimi Code, WorkBuddy, and Google.

## Stand-in search path

- Gate: `gwtool.WebSearch` (`internal/gwtool/gwtool.go:20`), served by `plugins/search` (`plugins/search/search.go:27`), checked by `searchServed()` (`search.go:585`).
- `translate` intercepts a client's search when the provider cannot search: `r = r.WithContext(searchingOn(r.Context(), p))` then `s.searchReply(...)` (`gateway.go:5576-5583`).
- `searchReply` (`search.go:905-923`) injects `searchTool` (`:507-515`), asks the model once, then runs up to `searchRounds = 6` (`:36`).
- `searchRounds` (`:925-1056`) collects tool calls, calls `s.webSearch` for each, sets `results[i].Text = found`, emits `KSearch`, appends assistant + tool-result messages, and re-asks.
- `webSearch` (`:525-574`) order:
  1. `ownSearcher` when the turn's provider searches for itself.
  2. `modelSearch` (`:598-664`) — the Settings searcher or `autoSearcher`; up to `searchersInTurn = 3`.
  3. `apiSearch` (`search_api.go:107-127`) — `provider.SearchAPIs()` in order, unless `SearchFirst == "api"`.
- `askSearchAPI` (`search_api.go:204-316`) vendors: Tavily, Brave, Exa, Firecrawl, You.com, SearXNG (`internal/provider/search_api.go:56-63`). `searchHits = 6`, `pageText = 1500` (`search_api.go:27-30`).
- `searchWith` (`search.go:784-857`) builds an Anthropic Messages body and calls `s.serve` in-process; found pages become `Hit`s and a "Sources:" list.
- Limits: `searchTimeout = 2m`, `searchWaitMost = 5m` (`search.go:42`); MCP body 1 MiB (`search_mcp.go:46`).

## Account and key selection

DeepSeek is key-based, not a signed-in account:

- `Provider.Key` (`provider.go:56`) and `Provider.Keys` (`provider.go:70-72`); `KeysOn()` (`provider/keys.go:116-140`).
- `perKeyBarred` (`fallback.go:175-288`) makes each key a candidate with `p.WithKey(k)` (`keys.go:368-383`); `rest` is `providerID#keyID` when there are several keys.
- `candidates`/`plan` (`fallback.go:334-376`) add fallbacks; `weigh`/`weighRouted` (`routing.go:1155,1184`) order them.
- The chosen candidate runs at `gateway.go:2652`; the served key is recorded at `gateway.go:2356-2364`.

The search path knows the serving account only partially:

- `searchOnKey` (`search_kimi.go:39`) is set with the candidate provider at `gateway.go:5582`, but `ownSearcher` reads it only when `searchesOwn(p)` (`search_kimi.go:73-76`) — Kimi, WorkBuddy, Google. Not for a generic/DeepSeek searcher.
- `searchWith` discards the key: it builds `"model": p.ID + "/" + model` (`search.go:869`) and re-enters `s.serve` (`:793`), so key selection runs again.
- `CallFor` (`search.go:60-91`) has no account field.

A DeepSeek key-aware search needs a new context value or a pinned-provider path.

## web_fetch

- Tool definition: `fetchTool` (`search_fetch.go:48-56`); handler `fetchMCPRun` (`:66-95`).
- `serveMCP` (`search_mcp.go:52-114`) requires `local(r)` or `sharedWith(r)` and rejects unlisted browser Origins. `fetchMCPRun` calls `budget.Reserve(access.Caller(r.Context()), len(raw), "", limitClock())` (`search_fetch.go:83`).
- Downloads via `fetchPage` (`:126-187`) with `webFetchClient = publicClient` (`:46`), which restricts to public addresses through every redirect.
- Limits: 30s, 5 MiB read, 20k chars default, 100k max, `offset` and `max_chars` arguments (`:38-43`).
- Wiring: `mux.HandleFunc(SearchMCPPath, s.searchMCP)` (`gateway.go:678`); `SearchMCPPath = "/mcp/magpie/web-search"` (`search_mcp.go:30`).
- The fetch path has no model, provider, or account awareness. There is no provider-specific fetch hook; the only seam is the package-level `webFetchClient` var, used by tests.

## Cost, usage, quotas

- A searcher-model call is a normal in-process gateway request, so it creates a `Call` and a `usage.Record` (`gateway.go:3405-3418`) with `Kind: "web_search"` (`codex_backend.go:1236`). Priced and written to `usage.jsonl` (`usage/usage.go:216-217`).
- Search-API calls (`askSearchAPI`) and Kimi/WorkBuddy search services are direct HTTP calls. They record no usage row and no cost; `metered` only applies `MaxRPM`.
- Tavily's `/usage` is read for quota display only (`search_quota.go:134-217`), never charged.
- MCP calls reserve against the calling gateway key's limit: `searchMCPRun` uses `reserveFor(...)` (`search_mcp.go:194`); `fetchMCPRun` uses `budget.Reserve(...)` (`search_fetch.go:83`).
- Existing non-token cost accounting: `usage.Record.OwnCost` and `Credits` (`usage/usage.go:76-80`). No per-search-API cost model exists.

## UI and CLI

- GUI search state: `searchState` (`internal/gui/api.go:535-575`) exposes `searchAPIs`, `searchVendors`, `searchProvider`, `searchChoices`, `searchAuto`, `searchUnused`, `searchEfforts`, `searchFailed`, `searchLeftOut`.
- Handlers: `POST /api/settings/search-api` (`api.go:2131`), `/search-key` (`:2161`), `/search-quota` (`:2177`); searcher validation at `api.go:1268-1274` requires membership in `gateway.Searchers()`.
- Settings fields: `Searcher`, `SearcherEffort`, `SearchFirst` (`internal/settings/settings.go:306-321`), validation at `:1289-1299`.
- `app.js`: `renderSearch` (`:20744`), searcher row `renderSearcher` (`:21021`), API-first radio (`:20816-20887`), provider "Searches the web by itself" toggle (`:9212-9216`, custom providers only).
- CLI: `magpie search` (`internal/cli/search_cli.go:20-52`), provider `search=yes|no` (`providers_cli.go:1046-1063`).

## Natural seams for DeepSeek

- Provider-level toggle: a field beside `Provider.Searches` (`provider.go:215`), rendered near `app.js:9212`; the `custom` guard would need to change for presets.
- Turn-aware search: `ownSearcher` (`search_kimi.go:73-79`) is the existing provider-scoped path.
- Pinning the serving key: `searchOnKey` exists (`search_kimi.go:39`) but `searchWith` re-enters `s.serve`; a DeepSeek search service could call `askDeepSeekSearch` directly with the chosen key instead.
- Provider-native fetch hook does not exist; `fetchMCPRun`/`webFetchClient` (`search_fetch.go:66,46`) need a new backend abstraction.
- Configurable searcher: `Settings.Searcher` and `Searchers()` (`search.go:360-384`) are where DeepSeek would be offered.

## Surprising constraints

- The stand-in searcher is chosen without the turn's model; only `ownSearcher` is turn-aware.
- Native search is protocol-limited: `Provider.Searches` is ignored on Chat. A Chat client offering `web_search_options` is forced off the relay path (`gateway.go:3546-3548`) and translated to a protocol where `searchesFor` is true.
- Custom relays/plugins said to search are never auto-picked; `searchRank` clears `Searches`.
- Searcher calls are recursive full gateway requests: `searchWith` calls `s.serve`, so routing, retries, usage, and pricing happen again. `searchingKey{}` prevents recursion.
- The turn's serving key is not pinned for searches. Even `searchOnKey` carries it, `searchWith` names only `provider/model`, and `CallFor` has no account field.
- Native search is not gated by the `plugins/search` switch.
- `web_fetch` is fully provider-agnostic: no model, provider, account, and only a public-address client.

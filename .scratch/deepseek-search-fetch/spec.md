Status: ready-for-agent

# DeepSeek provider search and fetch

## Problem Statement

A magpie user with a configured DeepSeek provider can chat through it, but cannot use DeepSeek's standalone web tools. The DeepSeek entry in 代搜供应商 (the searcher picker) asks a DeepSeek **model** to search through the Anthropic endpoint, which spends model tokens and returns only titles, URLs and page ages. It never calls DeepSeek's `/v1/tools/search`. `web_fetch` always downloads pages with magpie's own client, so DeepSeek's clean Markdown reader at `/v1/tools/fetch` is unreachable.

The two standalone APIs are billed per successful request ($0.75 / 5元 per 1,000). Using them automatically would spend money without the user choosing to, and today a tools-API call produces no usage row, so the spend would be invisible.

The same gap separates two magpies: a Remote magpie's own `web_search` and `web_fetch` cannot be used from here. Only its natively-searching models can serve as this magpie's searcher, model by model; its own search chain, its Search APIs and its fetch are unreachable.

## Solution

Link both standalone tools to a configured DeepSeek provider and its keys:

- **Search** becomes a manual-only service searcher in the existing Settings → Web search section, labeled “its web search”. Selecting it makes magpie call `/v1/tools/search` directly with the provider's key and feed the results through the existing `web_search` flow. It replaces the current model-based DeepSeek searcher entry.
- **Fetch** gets a surfaced “Fetch pages with” selector in the same section: magpie's own downloader (the default) or a configured DeepSeek provider. When DeepSeek is selected, `web_fetch` calls `/v1/tools/fetch`, applies the existing `offset`/`max_chars` slicing, and falls back to magpie's own downloader on transient failures.
- **Usage** records one row per successful call, with the $0.00075 USD per-request cost counted against the calling gateway key's limit.

The same two selectors also take a **Remote magpie**: each remote's `web_search` and `web_fetch` MCP tools can be selected directly, so the remote's whole search setup answers with its own keys. Image tools need nothing new — image recognition and image generation already pass through by picking the remote's models in the existing Settings pickers.

`/mcp/magpie/web-search` keeps serving both `web_search` and `web_fetch`; there is no new settings section or MCP server.

## User Stories

1. As a magpie user with a DeepSeek provider, I want magpie to recognise the provider's search API from the provider entry, so that I do not paste the same key twice.
2. As a magpie user, I want DeepSeek to appear as a service searcher in 代搜供应商, so that I can choose it for a model that cannot search.
3. As a magpie user, I want the DeepSeek searcher to be manual-only, so that magpie never spends money per search without my choosing it.
4. As a magpie user, I want the searcher labeled “its web search”, so that I know it uses the provider's own service rather than a model.
5. As a magpie user, I want the old model-based DeepSeek searcher entry replaced, so that there is only one unambiguous DeepSeek choice.
6. As a magpie user, I want the DeepSeek service entry to use the provider's existing key rotation, so that multi-key accounts behave like model calls.
7. As an agent, I want `web_search` to return DeepSeek's ranked results with title, URL and page text, so that I can answer with current information.
8. As an agent, I want up to 10 results, so that I have enough context without an oversized tool result.
9. As an agent, I want page text to prefer the full content, then the summary, then the snippet, so that I get the richest text DeepSeek offers.
10. As an agent, I want each page's text clipped to 1500 bytes, so that the tool result stays compact.
11. As an agent, I want the page date when DeepSeek provides one, so that I can judge recency.
12. As an agent, I want DeepSeek's ranking order preserved, so that its relevance ranking is not second-guessed.
13. As an agent, I want empty-URL results dropped, so that I never receive a page I cannot cite.
14. As an agent, I want an empty result set to fall through to the next search source, so that a failed query can still be answered.
15. As an operator, I want 401, 402, 403 and 429 responses to rest the DeepSeek key, so that a bad or exhausted key is not hammered.
16. As an agent, I want a clear message for a 400, so that I can correct the query.
17. As an agent, I want 408 and 5xx responses to use the existing search fallback, so that a transient DeepSeek failure does not end the search.
18. As a magpie user, I want a “Fetch pages with” selector in the Web search section, so that I can choose DeepSeek's page reader without visiting another settings page.
19. As a magpie user, I want magpie's own downloader to remain the default, so that fetch behaviour does not change until I opt in.
20. As a magpie user, I want the selector to name the configured DeepSeek provider, so that I can pick the account I intend to pay with.
21. As a magpie user, I want a warning and an automatic fallback when the chosen provider is removed, switched off or has no key, so that `web_fetch` keeps working.
22. As an agent, I want `web_fetch` to return DeepSeek's title and Markdown content, so that I can read the page it found.
23. As an agent, I want `max_chars` and `offset` to work the same with DeepSeek, so that long pages still arrive in parts.
24. As an agent, I want DeepSeek's title shown when it has one, so that I know which page I read.
25. As an agent, I want the 5 MiB read cap and 30-second timeout preserved, so that a huge or slow page cannot hang the tool.
26. As an agent, I want 408, 413, 429, 5xx and timed-out DeepSeek fetches to fall back to magpie's own downloader, so that a transient failure still returns a page.
27. As an agent, I want 402, 403 and 404 to surface DeepSeek's message without a fallback, so that balance, policy and empty-page cases are clear.
28. As a security-conscious user, I want private, loopback and link-local URLs refused before sending, so that magpie never asks DeepSeek to probe an internal target.
29. As a security-conscious user, I want URLs with a username or password refused, so that credentials are not sent to DeepSeek.
30. As a security-conscious user, I want search queries masked on the way out and restored for the agent, so that secrets in a query are not sent raw.
31. As an operator, I want one usage row per successful tools-API call with its cost, so that DeepSeek spend is visible in Usage and Routing.
32. As an operator, I want failures and empty results not to record a row or a cost, matching DeepSeek's billing.
33. As an operator, I want the per-request cost counted against the calling gateway key's limit, so that Key limits reflect real spend.
34. As an operator, I want distinct `web_search_api` and `web_fetch` kinds, so that tools-API calls are not confused with searcher-model `web_search` calls.
35. As an operator, I want DeepSeek's existing `/user/balance` display unchanged, so that I can still see the account balance.
36. As a magpie user, I want both choices inside the existing Web search section, so that I do not learn a new settings page.
37. As a magpie user, I want `/mcp/magpie/web-search` to keep listing both tools, so that my agent configuration does not change.
38. As a developer, I want the service searcher to be excluded from automatic selection, so that per-request charges never happen unasked.
39. As a developer, I want the provider's Chat base URL used for both tools endpoints, so that a compatible custom DeepSeek relay also works.
40. As a maintainer, I want the owning subsystem reference updated, so that the new searcher, selector and usage kinds are documented.
41. As a maintainer, I want the fork-only delivery recorded, so that no upstream PR is implied.
42. As a Claude Code user on DeepSeek, I want pass-through search unchanged, so that this work cannot break my existing setup.
43. As a magpie user with a Remote magpie provider, I want its web search offered as a service entry, so that its whole search setup answers without duplicating keys here.
44. As a magpie user, I want the remote service manual-only, so that the remote's keys are never spent unasked.
45. As a magpie user, I want a Remote magpie as a “Fetch pages with” option, so that pages are read with the remote's fetch setup.
46. As an agent, I want a remote search's answer text returned as my `web_search` result, so that I get its findings even though it carries no page list.
47. As an agent, I want `max_chars` and `offset` forwarded to a remote fetch, so that long pages still arrive in parts.
48. As an agent, I want a transient remote failure to fall back to magpie's own fetch, so that a slow or unreachable remote does not break `web_fetch`.
49. As an operator, I want a zero-cost local row for each pass-through, so that Routing shows the call while the remote's own usage carries the spend.
50. As an operator, I want the searching mark sent on pass-through calls, so that two magpies naming each other can never loop.
51. As a magpie user, I want image recognition and image generation to pass through by picking the remote's models, so that `analyze_image` and the image tools already work with no new setting.
52. As a developer, I want today's model-based remote search unchanged, so that a remote's natively-searching models keep working as searchers.
53. As a magpie user, I want a gone, off or keyless remote to warn and fall back, so that the tools keep working.
54. As an agent, I want a remote's error text surfaced, so that I can tell a refusal from a network failure.

## Implementation Decisions

### Service searcher

- A configured DeepSeek provider with a key is eligible for a provider-linked search service. Eligibility follows the provider's preset or its host, so a hand-added provider pointed at DeepSeek is recognised too. Each eligible provider gets its own service entry.
- `Searchers()` offers the provider as a `Service: true` choice with no model and no model list. The picker shows the existing service wording, “its web search”.
- `namedSearcherRank` gives the service the manual-only rank, as the Kimi Code service has: it is never picked by `autoSearcher`, `searchRank` or the fallback chain unless the user names it.
- The exclusion lives where the searcher rank is computed, **not** in the host lists: `searchesItself` and the Anthropic/Responses host lists keep their current answer, so Claude Code and Codex pass-through search is untouched.
- Intended behaviour change: a setup whose only searcher is DeepSeek stops auto-searching with a DeepSeek model. DeepSeek search happens only when the service is named in Settings.
- `chosenSearcher` returns the provider with an empty model for this service, so the existing no-model service branch runs. The no-model service branch is added wherever the Kimi service is special-cased today: the searcher list, the named-searcher parse, the spent check, and the per-searcher dispatch.
- The service replaces the current model-based DeepSeek searcher entry for that provider. A DeepSeek provider no longer appears as a list of searcher models.
- The service's search path calls the tools API directly with the provider's key; it does not build an Anthropic Messages request and does not spend model tokens.
- The provider's existing key list and `Pick` rotation are used. A refused key rests through the existing key-resting path.

### Search request and mapping

- The endpoint is the provider's Chat base URL with `/tools/search` appended; for the official preset that is `https://api.deepseek.com/v1/tools/search`.
- The request is `POST` with `Authorization: Bearer <key>`, JSON content type, and a body of `query`, `count: 10`, and `need_content: true`. `ori_question` and `localization` are omitted.
- The query goes through the existing masking path and the answer is restored for the agent, as other searches are.
- The request identifies itself as magpie's search, consistent with the existing search user agent.
- The response reader maps each result with a non-empty URL: title (falling back to the URL), text from `content`, then `summary`, then `snippet`, clipped to 1500 bytes at a rune boundary, and age from `date`.
- `site_name`, `site_icon`, `authority` and `rerank_score` are ignored. Results stay in DeepSeek's order; there is no quality filter.
- The existing numbered-page rendering and `Hit{Title, URL, PageAge}` conversion are reused.
- The tools-search caller reads up to 16 MiB of the response, above the search APIs' 4 MiB, because ten results with `need_content` can exceed the smaller cap. A reply cut at the cap fails as an unexpected reply and the search falls through.
- An empty result set returns the existing “found nothing” error, so the search fallback continues to the next source.
- The existing key-refusal statuses return the existing refused error, so the key rests. A 400 is surfaced as an invalid request. 408 and 5xx use the existing search fallback. No new retry loop is added.
- The existing search timeout applies.

### Fetch selector and handler

- A new settings value names the fetch backend: empty means magpie's own downloader, and a provider ID means that provider's fetch — DeepSeek's `/v1/tools/fetch` or a Remote magpie's `web_fetch`. The settings JSON exposes it beside the searcher fields under the key `fetch`.
- The settings JSON exposes the current value and the eligible provider choices, and the GUI renders a “Fetch pages with” picker in the existing Web search section. The picker offers “magpie's own” and each configured, on, keyed DeepSeek or Remote magpie provider by name.
- The value is validated against the same eligibility rule. A provider that is gone, off or keyless produces the existing “isn't used” warning and falls back to magpie's own downloader.
- `fetchMCPRun` chooses the backend: with no provider selected it calls the existing `fetchPage`; with a provider selected it calls that provider's fetch.
- The DeepSeek tools endpoint uses the provider's Chat base URL with `/tools/fetch` appended, the provider's key rotation, and a body of `{"url": <url>}`.
- URL checks: require http/https and a host; refuse a username or password; refuse literal loopback, private and link-local addresses and `localhost`. There is no local DNS resolution; DeepSeek resolves the public host.
- The response maps `title` and `content`: show the title when present, then the Markdown content, and apply the existing `offset` and `max_chars` limits (20,000 default, 100,000 maximum) to the content. The 5 MiB body cap and 30-second timeout are preserved. The `URL:` line names the requested URL; DeepSeek does not report the address its own redirects ended at.
- On 408, 413, 429 or 5xx, and on a transport error or the 30-second client timeout, fall back to magpie's own `fetchPage`; if that also fails, return the combined error. On 402, 403 or 404, surface DeepSeek's message and do not fall back. Other 4xx responses surface their message.
- The existing budget reservation against the calling gateway key happens before either backend.

### Remote magpie pass-through

- Any Remote magpie provider with a key is eligible. Each eligible remote gets its own manual-only service entry and its own fetch option, named by the provider.
- **Search:** selecting a remote's service entry sends a JSON-RPC `tools/call` for `web_search` to the remote's web-search MCP endpoint, with the provider's key as the gateway key. The remote's whole chain answers — its searcher, its Search APIs, its services — and its answer text becomes the local search result. The remote's MCP answer carries no structured page list, so the local result shows no Sources list.
- The pass-through request carries the existing searching mark, so the remote never forwards that search to another magpie's searcher; two magpies naming each other cannot loop.
- A remote transport failure or timeout is a failed source and the search falls through as any searcher failure does. A remote tool error surfaces its text.
- **Fetch:** selecting a remote in the fetch selector forwards `url`, `max_chars` and `offset` to the remote's `web_fetch`; the remote applies them and its answer text is returned as-is. A transport failure or timeout falls back to magpie's own downloader; a remote tool error result surfaces its text without a fallback.
- **Usage:** a pass-through records a local row with zero tokens and no cost — kind `web_search_api` or `web_fetch`, provider the remote, carrying the calling gateway key and agent. The remote's own usage row carries the real spend under its key.
- **Images:** no new surface. Image recognition and image generation already pass through by picking the remote's models in the existing Settings pickers; this spec documents that rather than adding a selector.
- Today's model-based remote search — a remote's natively-searching models — is unchanged and remains available beside the service entry.
- The local body-size budget reservation applies to pass-through calls as to any other.

### Usage and cost

- A successful tools-API call is an HTTP 200 with a usable result: a non-empty search result set, or fetch content. Only those calls create a usage record. A Remote magpie pass-through that answers successfully records a zero-cost row (above).
- Each record carries the DeepSeek provider, the key ID and name actually used, zero input and output tokens, and a cost of $0.00075 USD.
- The kind is `web_search_api` for search and `web_fetch` for fetch, distinct from the existing searcher-model `web_search` kind. The model field is empty because no model is called.
- The record carries the calling gateway key and the agent, as other MCP and side calls do, so the cost lands on the caller's key.
- The cost rides the existing per-call USD cost field on the usage record; its documentation is extended to say it also carries magpie's own per-request tool costs.
- The record's cost counts toward the calling gateway key's limit like token spend. The existing body-size reservation remains the pre-call concurrency and size guard; it is not the price.
- Failures and empty results create no record and no cost.
- The per-request price is billed from day one. The 2026-11-01 free window for server-side model tool calls is not encoded.
- DeepSeek's `/user/balance` provider display is unchanged; there is no separate search/fetch quota endpoint.

### Boundaries and documentation

- `/mcp/magpie/web-search` keeps serving both `web_search` and `web_fetch`; no new MCP server, tool or settings section is added.
- `web_fetch` stays independent of the search plugin and searcher configuration; only its backend changes when the selector names a provider.
- No model-protocol pass-through, per-model capability table or reply inspection is changed.
- The owning gateway routing reference is updated for the service searcher, the fetch selector, the remote pass-through, the mapping and the usage kinds. The provider/account reference is updated if the selector's eligibility or fallback wording changes what it documents.
- Delivery is fork-only; upstream code standards, tests and documentation conventions still apply.

## Testing Decisions

- **Seams confirmed with the user:** one end-to-end seam at the MCP endpoint (`/mcp/magpie/web-search` `tools/call`) with a fake DeepSeek HTTP server, and one settings-seam at the GUI's search state JSON. The remote pass-through uses the same MCP seam with a fake remote magpie. No new internal seam is introduced.
- **What makes a good test:** assert external behaviour only — the outgoing HTTP method, path, headers and JSON body; the tool result text and `isError`; the usage record's kind, provider, key, caller and cost; the settings JSON's value and choices. Do not assert private helpers, call counts of internal functions, or struct internals.
- **Modules tested:** the gateway's search and fetch path through the MCP server, the usage ledger, and the GUI search settings state.
- **Prior art:** the MCP tests `TestSearchMCPServesWebSearch`, `TestSearchMCPWithNoSearcher` and `TestSearchMCPHeldToKeyLimit`; the fetch tests `TestWebFetchReadsAPage` and `TestWebFetchOnlyPublic`; the service-searcher tests `TestKimiCodePlanNamedToSearch` and `TestKimiCodeSearchAddress`; the settings-state tests `TestSearchKeyShown` and `TestSearchAPIOrderAndOffFromSettings`; the remote-search tests `TestRemoteMagpieSearches` and `TestSearchingFromAnotherMagpie`.
- **Search cases:**
  - The DeepSeek service entry appears with no model and is not chosen automatically, including a setup whose only searcher is DeepSeek.
  - Selecting it sends `count: 10`, `need_content: true`, the masked query and the provider key, and does not create a model request.
  - `content`, `summary` and `snippet` map in that order; the 1500-byte clip and `date` mapping hold; empty URLs are dropped; order is preserved.
  - A reply larger than the read cap fails as an unexpected reply and falls through.
  - Empty results fall through; 401/402/403/429 rest the key; 400 surfaces; 408/5xx fall through.
- **Fetch cases:**
  - The selector defaults to magpie's own and offers each eligible provider, DeepSeek and Remote magpies alike.
  - Selecting DeepSeek sends the URL and key, returns title plus Markdown, and honours `offset` and `max_chars`; the `URL:` line is the requested URL.
  - Private and credential-bearing URLs are refused before any DeepSeek call.
  - 408/413/429/5xx and a client-side timeout fall back to magpie's own; 402/403/404 surface and do not fall back.
  - A gone, off or keyless provider shows the warning and uses magpie's own.
- **Remote pass-through cases:**
  - A remote's service entry appears manual-only with no model; selecting it posts the query to the remote's MCP tool with the provider key and the searching mark; the answer text becomes the tool result; no Sources list is added.
  - A remote fetch option forwards `url`, `max_chars` and `offset`; a transport failure falls back to magpie's own; a remote tool error surfaces its text.
  - A pass-through records a zero-cost row naming the caller and the remote provider.
  - A remote's natively-searching models keep working as searchers exactly as today.
- **Usage cases:**
  - A successful search and a successful fetch each create one row with the right kind, provider, key, caller and $0.00075 cost.
  - A failure and an empty result create no row.
  - The cost is counted against the calling key's limit.
  - A searcher-model call remains `web_search`, not `web_search_api`.
- **Regression:** with the fetch selector at magpie's own and no DeepSeek service selected, existing search APIs, searcher models and `web_fetch` behave exactly as before, and Anthropic/Responses pass-through for DeepSeek is unchanged.

## Out of Scope

- Model-protocol pass-through changes, per-model search support tables, and runtime detection or fallback for an ignored `web_search`.
- A standalone `deepseek` Search API vendor (`magpie search add deepseek`).
- A dedicated Web fetch settings section or a `/mcp/magpie/web-fetch` MCP server.
- A dedicated images pass-through selector; picking the remote's models in the existing pickers already covers image tools.
- Using DeepSeek's tools without a configured DeepSeek provider or a separate credential store.
- Hydrating search results with Fetch; search already carries content clipped at 1500.
- Encoding the 2026-11-01 free window for server-side model tool calls.
- New CLI commands for fetch; the selector is exposed through the existing settings and GUI surfaces.
- An upstream PR or merge into yetone/magpie.

## Further Notes

- The Kimi Code (China) service searcher is the precedent: a keyed, model-free service that appears as “its web search” and is manual-only for other models. DeepSeek differs in billing (per request rather than plan quota) and in having a fetch tool.
- The two tools remain on the same MCP endpoint; only their backend selection and result mapping change.
- The existing DeepSeek searcher entry is model-based and spends tokens, and today a DeepSeek-only setup can auto-pick it. This spec replaces it with a manual-only service; that auto-pick stops.
- A Remote magpie answers pass-through calls with the provider's key as its gateway key; the remote must be reachable and, from another machine, shared with that key.
- The effort's map and decision tickets live under `.scratch/deepseek-search-fetch/`.

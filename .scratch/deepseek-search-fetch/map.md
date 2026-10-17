# Wayfinder map: DeepSeek provider-linked search and fetch

## Destination

An implementation-ready spec for linking DeepSeek's standalone `/v1/tools/search` and `/v1/tools/fetch` APIs to a configured DeepSeek provider, landing in `gfunc/magpie` only:

- The provider's own account/key pays for the calls; no separate search-API entry and no new credential store.
- Search: DeepSeek is a manual-only service searcher in the existing Settings → Web search section, labeled "its web search"; selecting it calls `/v1/tools/search` and feeds the results through the existing `web_search` stand-in.
- Fetch: opt-in `/v1/tools/fetch` backend for the existing `web_fetch` tool, chosen with a surfaced fetch selector in the same Web search section; `/mcp/magpie/web-search` keeps serving both tools; magpie's own downloader remains the default.
- Both use the provider's existing key list and rotation.
- No model-protocol pass-through changes, no per-model capability table, no response inspection.
- Upstream-quality decisions, tests, and docs in the spec; no upstream PR.

## Notes

- Tracker: local markdown under `.scratch/deepseek-search-fetch/`.
- Verified external facts: [DeepSeek Search and Fetch APIs](research/deepseek-search-fetch-api.md).
- Existing seams: [magpie's search and web_fetch seams](research/search-fetch-seams.md).
- Kimi Code precedent: [Kimi Code (China)'s search service](research/kimi-code-cn-search.md) — a keyed, model-free service searcher; DeepSeek reuses the seam but is manual-only because each call is billed.
- Shape settled while charting:
  - The tools are provider-linked services, not model-protocol tools. Magpie is the HTTP caller; the model never sees the call.
  - `web_search` and `web_fetch` are the agent-facing tools; these APIs become their backends.
  - Both choices live in the existing Web search section; the fetch choice has its own surfaced selector.
  - No pass-through or model-gating changes.
  - Delivery is fork-only; the spec still meets upstream conventions.
- Consult `docs/subsystems/README.md`, `docs/subsystems/gateway-routing.md`, `docs/code-standards.md`, and `LESSONS.md` before designing.

## Decisions so far

- [Map magpie's search and web_fetch seams](issues/01-map-search-fetch-seams.md): two native layers exist; `ownSearcher` is turn-aware; `web_fetch` has no provider hook; `searchWith` loses the pinned key. The standalone tools API is a service magpie calls itself.
- [Map Kimi Code (China)'s search service on the 10.2.1.21 deployment](issues/09-map-kimi-code-cn-search.md): keyed, model-free `POST /coding/v1/search`; first for its own models, manual-only for others; 10.2.1.21 serves four `kimi-code-cn` models with a keyed plan and `web_search: magpie`; DeepSeek can reuse the seam but needs per-request cost/429 handling and should not auto-first.
- [Define how the DeepSeek provider links its search and fetch tools](issues/04-define-search-fetch-settings.md): manual-only service searcher labeled "its web search"; fetch has its own surfaced selector in the same section; both use provider key rotation; `/mcp/magpie/web-search` keeps both tools.
- [Map whether providers can give magpie MCP servers or tools](issues/10-map-provider-mcp-extension.md): MCP is one-way (magpie serves agents; `/mcp/{name}` is a Library sign-in proxy); no provider supplies servers/tools, and DeepSeek's search/fetch need core seams, not a provider plugin.
- [Map DeepSeek search results into the web_search flow](issues/05-map-deepseek-search-results.md): count 10, `need_content: true`; Text = content > summary > snippet clipped to 1500; Age = date; no filtering; reuse `searchRefused` and the existing fallback; fetch does not hydrate results.
- [Link DeepSeek Fetch to the provider as an opt-in web_fetch backend](issues/06-design-deepseek-fetch-backend.md): a "Fetch pages with" selector in the existing Web search section; `fetchMCPRun` branches to `/v1/tools/fetch`; scheme/host checks plus private-literal refusal; existing offset/max_chars and 5 MiB cap; transient failures fall back to magpie's own, 403/404 surface.
- [Decide cost and usage accounting for DeepSeek search and fetch](issues/07-decide-search-fetch-cost-accounting.md): a usage row per successful call at $0.00075 USD, distinct kinds `web_search_api`/`web_fetch`, counted toward the caller's key limit; failures and empty results record nothing; no free window encoded.
- [Write the DeepSeek search and fetch implementation spec](issues/08-write-implementation-spec.md): [spec.md](../spec.md) written and marked `Status: ready-for-agent`; seams are the MCP endpoint and the settings JSON.

## Not yet specified

None. The spec is written; implementation follows from it.

## Out of scope

- Per-model pass-through gating and the server-side support matrix ([Confirm DeepSeek's server-side search support matrix](issues/02-confirm-deepseek-native-support.md), [Choose when DeepSeek pass-through runs versus tools/search](issues/03-choose-pass-through-vs-tools-search.md)): this effort links the standalone tools to the provider and leaves model-protocol search untouched.
- A dedicated Web fetch settings section and `/mcp/magpie/web-fetch` MCP server: the existing `/mcp/magpie/web-search` already serves both `web_search` and `web_fetch`; the fetch choice is a selector in the existing section.
- A standalone `deepseek` search-API vendor (`magpie search add deepseek`): the key/account is the provider's.
- Using the tools without a configured DeepSeek provider: no separate credential store.
- Runtime detection or fallback for an ignored model-protocol `web_search`: status quo.
- An upstream PR or merge into `yetone/magpie`: delivery is fork-only.

# Decide cost and usage accounting for DeepSeek search and fetch

Status: resolved
Type: grilling
Blocked by: 05, 06

## Question

How should DeepSeek's per-request search and fetch cost be recorded and surfaced?

Facts:

- `$0.75 / 5元` per 1,000 successful requests, for both Search and Fetch, billed only on HTTP 200 with a usable result.
- Direct tools-API calls are documented as per-request billed; server-side tool calls through DeepSeek Harness/Claude Code/Codex are free until 2026-11-01. Clarify which side magpie's direct call falls on.
- Search-API calls today record no usage or cost (`askSearchAPI`); searcher-model calls record a normal `usage.Record` with `Kind: "web_search"` (`gateway.go:3405-3418`, `codex_backend.go:1236`).
- The budget plugin counts recorded cost (`plugins/budget/budget.go`); MCP calls reserve by body size (`search_fetch.go:83`, `search_mcp.go:194`).
- DeepSeek exposes `/user/balance` but no search/fetch usage endpoint.

Decisions to make:

- Does each tools-API call create a `usage.Record`? If so, what `Kind`, `Model`, provider key, and `OwnCost` does it carry?
- Is the cost `requests × $0.00075`, rounded how, and in which currency (USD, CNY, or both)?
- Does the budget plugin treat it like token spend, or is it informational only?
- Is the free window encoded, or assumed to end?
- What does the Usage/Routing UI show for a search/fetch call: a row, a count, or nothing?
- Does the DeepSeek balance API feed the provider's allowance/balance display separately?

## Answer

- **Record:** one `usage.Record` per successful tools-API call (HTTP 200 with a usable result), carrying the DeepSeek provider, the key used, zero input/output tokens, and the per-request cost. A failure or an empty result records nothing.
- **Price:** $0.00075 USD per successful call ($0.75 / 1,000), billed from day one. Do not encode the 2026-11-01 free window: that covers server-side model tool calls through Harness/Claude Code/Codex, not direct tools-API calls.
- **Budget:** the recorded cost counts toward the calling gateway key's limit like token spend. Keep the existing body-size reservation as the pre-call guard; it is not the price.
- **Kinds:** `web_search_api` for `/v1/tools/search` and `web_fetch` for `/v1/tools/fetch`, distinct from the existing `web_search` searcher-model kind.
- **UI:** each call appears as a Usage/Routing row with its cost. No separate search/fetch quota; DeepSeek's `/user/balance` remains the provider balance display.
- **Cost field:** the exact record field is for the spec. `OwnCost` is the existing USD per-call cost field, or an equivalent if its "client-reported" semantics do not fit.

# 14: Remote magpie search pass-through

**What to build:** Each keyed Remote magpie provider appears as a second manual-only service entry, “its web search”. Naming it forwards the query to the remote's own `web_search` MCP tool with the provider's key and the searching mark; the remote's whole chain — its searcher, its Search APIs, its services — answers, and its answer text becomes the local result with no Sources list. A local zero-cost usage row records the pass-through. Today's model-based remote search is unchanged.

**Blocked by:** 12 (DeepSeek service searcher behind web_search).

**Status:** resolved

- [x] Write `TestRemoteMagpieServiceSearches` first and watch it fail, against a fake remote magpie: naming the service posts a JSON-RPC `tools/call` for `web_search` to the remote's web-search MCP endpoint with the provider key; the answer text becomes the tool result; no Sources list is added.
- [x] `TestRemoteMagpieSearchMarksCall`: the pass-through request carries the searching mark, so the remote would never forward that search to another magpie's searcher.
- [x] `TestRemoteMagpieSearchFailures`: a transport failure or timeout is a failed source and the search falls through; a remote tool error surfaces its text.
- [x] `TestRemoteMagpieSearchRow`: a successful pass-through records a zero-cost row — kind `web_search_api`, provider the remote, zero tokens, the caller key and agent.
- [x] `TestRemoteMagpieNativeModelsUnchanged`: a remote's natively-searching models keep working as searchers exactly as today (`TestRemoteMagpieSearches` stays green unmodified).
- [x] The settings JSON shows the remote's service entry with no model, manual-only.

## Done

Merged as `b3f09072` on `deepseek-search-fetch`. Five tests red→green at a fake remote magpie (negative control `logs/14-red.log`); `TestRemoteMagpieSearches` green unmodified; full gateway suite ok 368s; vet clean (`logs/14.log`). Two spec-faithful deviations: the mark is backed by a no-forward refusal (the mark alone cannot stop two magpies naming each other's service entries from looping — covered in `TestRemoteMagpieSearchMarksCall`), and a keyed remote keeps its model-based entry beside the service entry, as the spec's "unchanged, beside" requires. Wave-4 gate: VET=0 TEST=0 (`logs/gate-14.log`).

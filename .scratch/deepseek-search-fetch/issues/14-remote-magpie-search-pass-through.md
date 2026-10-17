# 14: Remote magpie search pass-through

**What to build:** Each keyed Remote magpie provider appears as a second manual-only service entry, “its web search”. Naming it forwards the query to the remote's own `web_search` MCP tool with the provider's key and the searching mark; the remote's whole chain — its searcher, its Search APIs, its services — answers, and its answer text becomes the local result with no Sources list. A local zero-cost usage row records the pass-through. Today's model-based remote search is unchanged.

**Blocked by:** 12 (DeepSeek service searcher behind web_search).

**Status:** ready-for-agent

- [ ] Write `TestRemoteMagpieServiceSearches` first and watch it fail, against a fake remote magpie: naming the service posts a JSON-RPC `tools/call` for `web_search` to the remote's web-search MCP endpoint with the provider key; the answer text becomes the tool result; no Sources list is added.
- [ ] `TestRemoteMagpieSearchMarksCall`: the pass-through request carries the searching mark, so the remote would never forward that search to another magpie's searcher.
- [ ] `TestRemoteMagpieSearchFailures`: a transport failure or timeout is a failed source and the search falls through; a remote tool error surfaces its text.
- [ ] `TestRemoteMagpieSearchRow`: a successful pass-through records a zero-cost row — kind `web_search_api`, provider the remote, zero tokens, the caller key and agent.
- [ ] `TestRemoteMagpieNativeModelsUnchanged`: a remote's natively-searching models keep working as searchers exactly as today (`TestRemoteMagpieSearches` stays green unmodified).
- [ ] The settings JSON shows the remote's service entry with no model, manual-only.

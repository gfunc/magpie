# 12: DeepSeek service searcher behind web_search

**What to build:** With a keyed DeepSeek provider configured, the Web search section offers “DeepSeek · its web search” as a manual-only service entry with no model. Naming it makes a `web_search` call — an agent's MCP call or a model's stand-in — ask DeepSeek's tools/search API directly with the provider's key: ten results with content, mapped to numbered pages, and recorded as a $0.00075 usage row on the caller's key. The old model-based DeepSeek searcher entry is gone, and DeepSeek's Anthropic/Responses pass-through is untouched.

**Blocked by:** 11 (Prefactor the service-searcher seam).

**Status:** ready-for-agent

- [ ] Write `TestDeepSeekServiceSearches` first and watch it fail: naming the service posts to the provider's tools/search with `count: 10`, `need_content: true`, the masked query and the provider key; no DeepSeek model request is made; the tool result is the numbered pages.
- [ ] `TestDeepSeekServiceMapsResults`: text maps from content, then summary, then snippet; the 1500-byte clip lands at a rune boundary; `date` becomes the page age; empty URLs are dropped; DeepSeek's order is kept; site, authority and rerank are ignored.
- [ ] `TestDeepSeekServiceManualOnly`: the settings JSON shows the entry with no model and it is never auto-picked, including when DeepSeek is the only searcher; the model-based entry is gone.
- [ ] `TestDeepSeekSearchFailures`: empty results fall through to the next source; 401/402/403/429 rest the key; 400 surfaces its message; 408/5xx fall through; a reply past the 16 MiB read cap fails as an unexpected reply and falls through.
- [ ] `TestDeepSeekSearchUsageRow`: one row per successful call — kind `web_search_api`, the provider and key used, zero tokens, the caller key and agent, $0.00075; failures and empty results record nothing.
- [ ] `TestDeepSeekPassThroughUnchanged`: an Anthropic-protocol request to DeepSeek still passes the `web_search` tool through exactly as today.
- [ ] The gateway routing reference's web-search section documents the service entry and its manual-only rank.

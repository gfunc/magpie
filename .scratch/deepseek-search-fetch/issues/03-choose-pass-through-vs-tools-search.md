# Choose when DeepSeek pass-through runs versus tools/search

Status: resolved
Type: grilling
Blocked by: 02

## Question

When should magpie delegate a DeepSeek turn's web search to DeepSeek's own endpoint, and when should it call `/v1/tools/search` itself?

Settled already: keep pass-through where DeepSeek executes the search, and supplement where it does not. What is not settled is the gate:

Facts: [DeepSeek server-side search support matrix](../research/deepseek-native-support.md) — the Anthropic endpoint executes web search server-side (`deepseek-flash` documented; `deepseek-v4-pro` unverified); the Responses API honors `web_search` only on `deepseek-v4-pro` and silently ignores it on `deepseek-flash`.

- Should the gate be model-aware (for example, `deepseek-v4-pro` on Responses may pass through; `deepseek-flash` may not)?
- Where does that capability live: a hardcoded model list, catalog metadata, the provider's `Searches` flag, or a new provider field?
- On the Anthropic endpoint, should magpie pass through for every model, or treat unverified Pro support as a reason to supplement?
- When magpie supplements, does it suppress the client's `web_search` tool and answer it with `searchReply`, or send both?
- What should the user see when pass-through was chosen but DeepSeek ignored the tool?

## Exit condition

A rule magpie can implement without guessing: protocol + model + capability → pass-through or tools/search, including the fallback when the vendor ignores the tool.

## Answer

- **Gate:** model-aware per API. On Responses, only `deepseek-v4-pro` passes through; `deepseek-flash` uses the supplement. On Anthropic, all models pass through (flash documented; Pro unverified but the endpoint's table has no model qualifier).
- **Capability source:** a code table in the `searchModels` style (`internal/gateway/search.go:163-199`). Remove `api.deepseek.com` from `searchHosts[Responses]`; add a `deepseek` entry listing `deepseek-v4-pro`; keep `api.deepseek.com` in `searchHosts[Anthropic]`; add the host fallback in `searchModelsHosts` for hand-added providers.
- **Supplement:** for flash on Responses, Chat, and any model not allowed to pass through, remove the client's `web_search` tool and answer it through the existing stand-in path using `/v1/tools/search`. One search, one bill.
- **Pass-through detection:** keep the status quo. Do not inspect replies for `web_search_tool_result`/`web_search_call`, do not retry, and add no new warning. The gate handles the known flash case; unverified Pro-Anthropic is accepted as-is.
- **Fetch:** `web_fetch` stays in scope. DeepSeek Fetch backs it only when opted in; magpie's own downloader remains the default (ticket 06).

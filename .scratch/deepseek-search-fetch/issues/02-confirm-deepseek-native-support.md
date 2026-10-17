# Confirm DeepSeek's server-side search support matrix

Status: resolved
Type: research

## Question

Which DeepSeek models and protocols actually execute a server-side `web_search` tool, and what does each return?

Specifically:

- On `https://api.deepseek.com/anthropic` (Anthropic protocol), which models execute `web_search_20250305`? Does the result come back as `web_search_tool_result` blocks with pages magpie can render?
- On the Responses API, `web_search` is documented as working only on `deepseek-v4-pro` and ignored on `deepseek-flash`. Verify that, including whether the reply says anything when the tool is ignored.
- Does the Anthropic-format endpoint have the same model restriction? The docs mention Claude Code, Codex, and DeepSeek Harness server-side calls hitting Search/Fetch, but do not give a per-model matrix.
- What does the tool result look like (titles, URLs, snippets, citations), and does it differ from the standalone `/v1/tools/search` shape?

## Why it matters

This decides when pass-through is safe and when magpie must supplement with `/v1/tools/search`. Without it, a DeepSeek Responses turn on `deepseek-flash` may silently search nothing.

## Answer

DeepSeek's support is split by protocol, not uniform:

- **Anthropic Messages:** web search runs server-side. `server_tool_use` and
  `web_search_tool_result` are marked supported with no model qualifier;
  `deepseek-flash` is documented end to end via Claude Code. `deepseek-v4-pro`
  on this endpoint is **not verified** (no page excludes or demonstrates it).
  Results carry `title`, `url`, `page_age`, and vendor-sealed
  `encrypted_content` — no snippet and no documented citations. Magpie's
  pass-through relays those bytes; cross-protocol rendering keeps title, URL,
  and age and drops the seal.
- **Responses:** `web_search` is `deepseek-v4-pro` only. On `deepseek-flash` it
  is **silently ignored** — no error, no note — and the model answers without
  searching. DeepSeek's own Codex page contradicts itself: its prose says Pro
  only, while the `models.json` sample sets `supports_search_tool: true` for
  flash.
- **Standalone `/v1/tools/search`** returns many more fields (`snippet`,
  `summary`, `content`, `rerank_score`, `site_name`, `date`, `authority`) and
  defaults to 10 results (max 50). The server-side blocks do not.
- **Billing:** standalone calls are billed now at $0.75 per 1,000 successful
  requests. Only server-side calls via Harness, Claude Code, or Codex are free,
  until 2026-11-01 00:00 Beijing (2026-10-31 16:00 UTC).

The silent `deepseek-flash` + Responses case is real because magpie's
`searchesFor` is model-agnostic for DeepSeek: it sends the tool and cannot tell
that DeepSeek ignored it.

Full matrix, quotes, and gaps: [DeepSeek server-side search support](../research/deepseek-native-support.md).

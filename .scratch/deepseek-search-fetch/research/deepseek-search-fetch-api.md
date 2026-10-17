# DeepSeek Search and Fetch APIs

Verified 2026-10-17 from the DeepSeek API docs.

Sources:

- https://api-docs.deepseek.com/zh-cn/api/search (English: https://api-docs.deepseek.com/api/search)
- https://api-docs.deepseek.com/zh-cn/api/fetch (English: https://api-docs.deepseek.com/api/fetch)
- https://api-docs.deepseek.com/guides/search
- https://api-docs.deepseek.com/quick_start/pricing
- https://api-docs.deepseek.com/quick_start/rate_limit
- https://api-docs.deepseek.com/updates

## Endpoints

| API | Method and path | Behaviour |
| --- | --- | --- |
| Search | `POST https://api.deepseek.com/v1/tools/search` | Search the web for a query; ranked structured results. |
| Fetch | `POST https://api.deepseek.com/v1/tools/fetch` | Fetch one page; title and Markdown content. |

Both are non-streaming raw JSON APIs. Auth is `Authorization: Bearer <DeepSeek API key>` plus `Content-Type: application/json`; the same platform key as the model API. Errors are OpenAI-shaped with `param: null`.

## Search request

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `query` | string | yes | — | 1–128 Unicode chars; operators `site:`, `-site:`, `-`, `-"..."`, `"..."`, `lang:`, `after:`, `before:` |
| `count` | integer | no | 10 | 1–50 |
| `ori_question` | string/null | no | — | 1–128 chars |
| `localization` | string | no | `""` | An IP address used to localize results |
| `need_content` | boolean | no | false | When false, `content` is always `""` |

Unknown request fields are ignored.

## Search response

Top level is `{ "results": [...] }`; an empty array when nothing matched. Each result:

`rerank_score` (0–3, ≥1.5 high quality), `title`, `url`, `snippet` (100–200 chars), `summary` (600–800 chars, intended for a model), `content` (empty unless `need_content`), `site_name`, `site_icon`, `date` (RFC 3339 UTC, nullable), `authority` (0–4, -1 unknown, ≥3 highly authoritative).

Example:

```json
{
  "results": [
    {
      "rerank_score": 2.4,
      "title": "DeepSeek V4 model released",
      "url": "https://example.com/deepseek-v4",
      "snippet": "DeepSeek V4 is now available, with tool calling and long-context support.",
      "summary": "DeepSeek has released the V4 model. The release adds improved tool calling and long-context support for agent workflows.",
      "content": "DeepSeek V4 is now generally available...",
      "site_name": "Example News",
      "site_icon": "https://example.com/favicon.ico",
      "date": "2026-06-01T00:00:00+00:00",
      "authority": 3
    }
  ]
}
```

## Fetch request and response

Request: `{ "url": "https://..." }`. The URL is at most 8192 characters, must be http/https with a host, and must not contain a username or password.

Response: `{ "title": "...", "content": "# ... (Markdown) ..." }`. `title` may be empty; fetching is subject to the target site's availability and access policy.

## Errors

400 invalid request, 401 bad key, 402 insufficient balance, 403 Fetch rejected by security policy, 404 Fetch found no usable content, 408 body read timeout, 413 body too large, 429 QPS exceeded, 500 internal, 504 downstream timeout.

Search and Fetch are read-only. Retry 408/429/5xx after waiting; top up on 402; do not retry other 4xx unchanged.

## Billing

Per successful request, not per token or result:

| API | Unit | Price |
| --- | --- | --- |
| Search | 1,000 successful requests | $0.75 / 5元 |
| Fetch | 1,000 successful requests | $0.75 / 5元 |

Billable only on HTTP 200 with a usable result (non-empty `results`; Fetch returns page content). Failures, empty search results, and timeouts are not billed. `count` does not affect price.

Server-side tool calls through DeepSeek Harness, Claude Code, or Codex already hit these APIs and are free until 00:00 Beijing time on 2026-11-01; per-request billing starts then.

## Rate limits

Per account, independent of model concurrency: Search 5 QPS; Fetch 5 QPS. Fetch also has a per-domain QPS limit (value not documented). Exceeding either returns 429.

## Model and protocol support

- The Search/Fetch docs describe standalone HTTP APIs; they do not document function schemas for Chat Completions.
- Chat Completions tool use is client-side: the model emits `tool_calls`; it does not execute them.
- The Responses API has a server-side `web_search` tool, but it works only with `deepseek-v4-pro` and is ignored with `deepseek-flash`.
- MCP is listed among Responses built-in tools that are ignored.
- No MCP server exposing Search/Fetch is documented.

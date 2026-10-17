# Link DeepSeek Fetch to the provider as an opt-in web_fetch backend

Status: resolved
Type: grilling

## Question

How should the DeepSeek provider's `POST /v1/tools/fetch` become an opt-in backend for magpie's `web_fetch` tool, chosen with a surfaced selector in the existing Web search section?

Settled already:

- The API is linked to the DeepSeek provider and paid by its key/rotation.
- Fetch is opt-in; magpie's own downloader remains the default.
- No new settings section and no new MCP server: the fetch choice is a surfaced picker in the existing Settings → Web search section, and `/mcp/magpie/web-search` keeps serving both `web_search` and `web_fetch`.
- Search already requests `need_content: true` and clips each page to 1500, so Fetch is only for the `web_fetch` tool, not search-result hydration.

## Answer

- **Surface:** a "Fetch pages with" picker in the existing Settings → Web search section, beside the searcher: magpie's own (default) or DeepSeek. It is a normal selector with its own value, not a hidden provider toggle. `web_fetch` is already listed by `/mcp/magpie/web-search`; no new tool, endpoint, section, or interface is added.
- **Code shape:** a setting value (proposed `Settings.Fetch`: empty for magpie's own, `deepseek` for the provider), exposed through `searchState` and the GUI picker, and a branch in `fetchMCPRun` (or a small `fetchWith` helper) that calls `POST /v1/tools/fetch` with the provider's key, or `fetchPage` as today.
- **Keys:** the provider's existing key list and `Pick` rotation.
- **URL checks:** require http/https and a host, refuse a username/password, and refuse literal loopback, private and link-local addresses plus `localhost`. No local DNS resolution; DeepSeek resolves the public host itself.
- **Request:** `{"url": <checked url>}`.
- **Response:** show the title when present, then the Markdown content. Apply the existing `max_chars` and `offset` (default 20000, max 100000), read the response body up to 5 MiB, and reuse the existing 30-second fetch timeout.
- **Failures:** 408, 413, 429 and 5xx fall back to magpie's own `fetchPage`; if that also fails, return the combined error. 403 (security policy) and 404 (no usable content) surface DeepSeek's message with no fallback.
- **Budget:** keep `budget.Reserve(access.Caller(...), len(url), ...)` before either backend.
- **Provider off/removed/key removed:** the selector shows the existing "isn't used" warning and falls back to magpie's own downloader.
- **Search interaction:** none. Fetch only serves `web_fetch`; search results already carry content clipped at 1500.

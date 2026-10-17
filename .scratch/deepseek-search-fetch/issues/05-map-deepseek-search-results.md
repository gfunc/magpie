# Map DeepSeek search results into the web_search flow

Status: resolved
Type: grilling

## Question

How should `POST /v1/tools/search` map into magpie's `foundPage` and the stand-in `web_search` flow?

Decisions to make:

- Request shape: `query`, `count`, `need_content`, `ori_question`, `localization`. What `count` (magpie's `searchHits` is 6; DeepSeek's default is 10, max 50)? Is `need_content: true` worth the larger response, or should magpie use `summary` and fetch only when the model needs more?
- Result mapping: which fields become `foundPage.Title`, `URL`, and `Text` — `summary` (600–800 chars, model-facing), `content` (opt-in), or `snippet`? Is `site_name` shown? Does `date` map to `foundPage.Age`? Should `rerank_score` or `authority` filter low-quality hits?
- Result count and truncation: `searchHits = 6`, `pageText = 1500` (`internal/gateway/search_api.go:27-30`). Do these hold for DeepSeek, or does its `summary`/`content` need a different cap?
- Error mapping: 400, 401, 402, 403, 404, 408, 413, 429, 500, 504 into `searchRefused`, resting, and the model-visible message. Search has no 403/404; Fetch does.
- Timeouts and retries: the existing `searchTimeout = 2m` / `searchWaitMost = 5m`; retry 408/429/5xx, not other 4xx.
- Sources rendering: does DeepSeek's result set fit the existing "Sources:" list, or does it need citations/site names?

## Answer

- **Request:** `POST /v1/tools/search` with `{"query": <masked query>, "count": 10, "need_content": true}`. Omit `ori_question` and `localization`.
- **Mapping:** for each result with a non-empty URL, `Title` = `title` (URL if empty), `Text` = `content`, else `summary`, else `snippet`, clipped to `pageText` (1500 bytes, at a rune); `Age` = `date`. Ignore `site_name`, `site_icon`, `authority`, and `rerank_score`; keep DeepSeek's order, with no quality filter.
- **Rendering:** `pagesSaid` numbers the pages and produces `Hit{Title, URL, PageAge}`. No citations or site names are added beyond the existing numbered list.
- **Empty results:** return `errFoundNothing` for that source, so the existing `webSearch` fallback continues to the next searcher or Search API, as `apiSearch` does.
- **Errors:** the existing `keyRefusal` set (401/402/403/429) returns `searchRefused`, so the key rests; 400 is a user-visible invalid request; 408/429/5xx use the existing fallback/retry path. No new retry loop.
- **Timeouts:** reuse `searchTimeout` (2m) and the existing fallback chain.
- **Fetch:** not used to hydrate search results. `web_fetch` remains for reading a page beyond the 1500-byte clip.

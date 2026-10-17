# Define how the DeepSeek provider links its search and fetch tools

Status: resolved
Type: grilling
Blocked by: 09

## Question

How should a configured DeepSeek provider opt into its standalone search and fetch tools, and which account pays?

Settled already:

- The APIs are linked to the provider; magpie calls them with the provider's key.
- The tools are model-independent; no model-protocol pass-through changes.
- Search and fetch have separate choices, mirroring `Settings.Searcher` and the image-understanding model.
- Fetch is opt-in; magpie's own downloader remains the default.

Decisions to make:

- Is search automatic for every DeepSeek provider, behind a provider-level switch, or only when the provider is chosen as the searcher in Settings?
- Which key pays: the provider's first key, a chosen key, the key serving the turn, or the provider's existing key rotation? DeepSeek limits each account to 5 QPS per API.
- What are the exact search and fetch settings: field names, defaults, validation, and what happens when the provider is off, removed, or loses its key?
- Where do they live: the provider editor, Settings → Web search, Settings → Images (the fetch analogy), CLI `magpie search` / `provider`, or a mix?
- What does the UI call them — "its search API", "search backend", "fetch backend" — and how does that avoid confusion with the existing Searcher setting?

## Answer

- **Search:** stays in the existing Settings → Web search section. DeepSeek appears as a `Service: true` entry with no model, labeled "its web search", and is **manual-only**: `Searchers()` offers it, `namedSearcherRank` keeps it out of automatic selection, and it is never auto-picked. Selecting it calls `POST /v1/tools/search`; no DeepSeek chat request. This replaces the current model-based DeepSeek searcher entry.
- **Fetch:** a **surfaced selector** (a second picker row, not a hidden provider toggle) in the same Settings → Web search section, labeled "Fetch pages with": magpie's own downloader (default) or DeepSeek. `/mcp/magpie/web-search` keeps serving both `web_search` and `web_fetch`; no new section, endpoint, or MCP server.
- **Keys:** both search and fetch use the provider's existing key list and `Pick` rotation. No new key field. A refused key rests as it does for model calls.
- **Defaults:** search is manual-only, so no automatic spend; fetch stays magpie's own until chosen.
- **Provider off/removed/key removed:** the DeepSeek search entry disappears; the fetch selector shows its "isn't used" warning and falls back to magpie's own.
- **Labels:** "its web search" for the service entry; "Fetch pages with" for the fetch selector.
- **Left to ticket 06:** the exact picker options/copy and URL safety.

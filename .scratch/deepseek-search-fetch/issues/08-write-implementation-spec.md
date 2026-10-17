# Write the DeepSeek search and fetch implementation spec

Status: resolved
Type: task
Blocked by: 04, 05, 06, 07

## Question

Once the decision tickets are resolved, assemble them into an implementation-ready spec at `.scratch/deepseek-search-fetch/spec.md`.

The spec must cover:

- Behaviour: how a DeepSeek provider links its search and fetch tools, search activation, fetch opt-in, defaults and toggles.
- Settings and account selection, with exact fields, defaults, validation, and UI/CLI surfaces.
- Surfaces: the existing Settings → Web search section gains a surfaced fetch selector; `/mcp/magpie/web-search` keeps serving both `web_search` and `web_fetch`; no new section or MCP server.
- Code seams: which functions and files change (`ownSearcher` or a new provider-linked path, `webSearch`/`searchReply`, `fetchMCPRun`/`webFetchClient`, `Settings`, `searchState`, `magpie search`), with source links.
- Request/response mapping and error/retry behavior for both APIs.
- Cost accounting and budget interaction.
- Tests: which existing tests exercise the paths, and the new coverage needed.
- Docs: the owning subsystem reference under `docs/subsystems/`, updated in the same change per `docs/subsystems/README.md`.
- Fork-only delivery note: no upstream PR, but upstream code standards still apply.

## Answer

The spec is written at [spec.md](../spec.md), marked `Status: ready-for-agent`.

Test seams confirmed with the user:

- the MCP endpoint `/mcp/magpie/web-search` `tools/call`, with a fake DeepSeek HTTP server, for both tools and the usage row;
- the GUI search-state JSON for the service searcher entry and the fetch selector.

No Go code changed.

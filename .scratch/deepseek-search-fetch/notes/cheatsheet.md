# Harness cheat-sheet: DeepSeek search/fetch queue

Repo: the magpie fork, integration branch `deepseek-search-fetch`. Work only in your own worktree, based on that branch; never push; never touch the main checkout.

## Commands

- Full gate (orchestrator runs this; tickets do not): `make test` — `go vet` + `go test` with the Linux tags (`production gtk3`).
- Targeted test: `go test -tags 'production gtk3' ./internal/gateway/ -run 'TestName' -count=1`.
- GUI browser tests (only waves that touch `internal/gui/assets`): `make test-ui`.
- Build check: `go build -tags 'production gtk3' ./...`.
- Long checks: tee output to `/home/jason/projects/magpie/.scratch/deepseek-search-fetch/logs/<ticket>.log` (absolute path from your worktree).

## Code standards that bite (docs/code-standards.md)

- Every fix ships with a test that fails without it; prove the negative control before implementing.
- Tests never reach the network: fake the vendor with `httptest`, built from bytes shaped like the real replies.
- Tests use real-shaped inputs (missing fields, empty values, defaults).
- A fake server's handler runs on many goroutines: guard what it records with a lock.
- No `rm -rf` in commands; use precise targets (`rm -f path/*.log`).
- Read `LESSONS.md` before changing code.

## Seams (agreed in the spec)

- End-to-end: the MCP endpoint `/mcp/magpie/web-search` `tools/call`, prior art `TestSearchMCPServesWebSearch`, `TestWebFetchReadsAPage`.
- Settings: the GUI search-state JSON, prior art `TestSearchKeyShown`, `TestSearchAPIOrderAndOffFromSettings`.
- Service-searcher prior art: `TestKimiCodePlanNamedToSearch`, `TestKimiCodeSearchAddress`; remote prior art: `TestRemoteMagpieSearches`, `TestSearchingFromAnotherMagpie`.

## Key symbols

- Searcher chain: `Searchers`, `namedSearcherRank`, `chosenSearcher`, `searcherSpent`, `searchBy`, `webSearch`, `apiSearch`, `searchesItself`, `searchHosts`.
- Fetch: `fetchMCPRun`, `fetchPage`, `webFetchClient`, `fetchTool`.
- Usage: `usage.Record` fields `Kind`, `OwnCost`, `CallerKeyID`; kinds `web_search` (model) vs new `web_search_api`/`web_fetch`.
- Settings: `Searcher`, `SearcherEffort`, `SearchFirst`; the new fetch value is exposed as `fetch` in the search state.

## Environment quirks

- The gateway may be running from this repo (`magpie serve`); never restart or reconfigure it from a ticket.
- Go caches and `HOME` in tests: follow the existing test sandbox helpers; never touch `~/.config/magpie`.

## Verification ceiling

Run exactly the checks your ticket lists and stop. Wave gating, full e2e and integration verification belong to the orchestrator.

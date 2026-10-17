# 13: Fetch selector with the DeepSeek fetch backend

**What to build:** The Web search section gains a “Fetch pages with” picker — magpie's own downloader (the default) or a keyed DeepSeek provider. With DeepSeek chosen, a `web_fetch` call sends the checked URL to the provider's tools/fetch API and returns the title plus Markdown under the existing offset/max_chars limits, falls back to magpie's own downloader on transient failures, and records a $0.00075 usage row. A gone, off or keyless provider warns and falls back.

**Blocked by:** 12 (DeepSeek service searcher behind web_search).

**Status:** resolved

- [x] Write `TestFetchSelectorDefaultsOwn` first and watch it fail: the settings JSON exposes the `fetch` value and its choices; the default is magpie's own; existing fetch behavior is unchanged.
- [x] `TestDeepSeekFetchReadsPage`: the call sends the URL with the provider key; the title plus Markdown return; `offset` and `max_chars` (20,000 default, 100,000 max) apply; the 5 MiB cap and 30-second timeout hold; the `URL:` line names the requested URL.
- [x] `TestDeepSeekFetchRefusesURLs`: literal loopback, private and link-local addresses, `localhost`, and URLs with a username or password are refused before any DeepSeek call.
- [x] `TestDeepSeekFetchFallbacks`: 408, 413, 429, 5xx, a transport error and a client timeout fall back to magpie's own downloader; 402, 403 and 404 surface DeepSeek's message; both backends failing returns the combined error.
- [x] `TestDeepSeekFetchUsageRow`: one row per successful fetch — kind `web_fetch`, the provider and key, zero tokens, the caller, $0.00075; failures record nothing; the body-size budget reservation still runs before either backend.
- [x] `TestFetchProviderGoneFallsBack`: a removed, off or keyless provider shows the “isn't used” warning and uses magpie's own downloader.
- [x] The Web search section's documentation describes the picker and both options.

## Done

Merged as `a30b0b3d` on `deepseek-search-fetch`. Five gateway tests red→green at a fake fetch site plus a new settings-state test; negative controls recorded. One shared root fix in the usage ledger: zero-token rows' own cost now counts toward a key's limit (also corrects ticket 12's rows), caught by this ticket's red test. Full gateway suite ok 369s; targeted gui/settings tests ok; vet clean (`logs/13.log`). Wave-3 gate: VET=0 TEST=0; `make test-ui` **not runnable on this machine** — `Cannot find module 'playwright'`, every file fails at require (`logs/gate-13-ui.log`); the touched JS passes `node --check`.

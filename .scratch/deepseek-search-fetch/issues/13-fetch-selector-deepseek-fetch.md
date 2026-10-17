# 13: Fetch selector with the DeepSeek fetch backend

**What to build:** The Web search section gains a “Fetch pages with” picker — magpie's own downloader (the default) or a keyed DeepSeek provider. With DeepSeek chosen, a `web_fetch` call sends the checked URL to the provider's tools/fetch API and returns the title plus Markdown under the existing offset/max_chars limits, falls back to magpie's own downloader on transient failures, and records a $0.00075 usage row. A gone, off or keyless provider warns and falls back.

**Blocked by:** 12 (DeepSeek service searcher behind web_search).

**Status:** ready-for-agent

- [ ] Write `TestFetchSelectorDefaultsOwn` first and watch it fail: the settings JSON exposes the `fetch` value and its choices; the default is magpie's own; existing fetch behavior is unchanged.
- [ ] `TestDeepSeekFetchReadsPage`: the call sends the URL with the provider key; the title plus Markdown return; `offset` and `max_chars` (20,000 default, 100,000 max) apply; the 5 MiB cap and 30-second timeout hold; the `URL:` line names the requested URL.
- [ ] `TestDeepSeekFetchRefusesURLs`: literal loopback, private and link-local addresses, `localhost`, and URLs with a username or password are refused before any DeepSeek call.
- [ ] `TestDeepSeekFetchFallbacks`: 408, 413, 429, 5xx, a transport error and a client timeout fall back to magpie's own downloader; 402, 403 and 404 surface DeepSeek's message; both backends failing returns the combined error.
- [ ] `TestDeepSeekFetchUsageRow`: one row per successful fetch — kind `web_fetch`, the provider and key, zero tokens, the caller, $0.00075; failures record nothing; the body-size budget reservation still runs before either backend.
- [ ] `TestFetchProviderGoneFallsBack`: a removed, off or keyless provider shows the “isn't used” warning and uses magpie's own downloader.
- [ ] The Web search section's documentation describes the picker and both options.

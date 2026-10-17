# 15: Remote magpie fetch option

**What to build:** The “Fetch pages with” picker also offers each keyed Remote magpie. Choosing one forwards `url`, `max_chars` and `offset` to the remote's `web_fetch` tool; the remote applies them and its answer text returns as-is. A transport failure or timeout falls back to magpie's own downloader; a remote tool error surfaces. A zero-cost usage row records the pass-through.

**Blocked by:** 13 (Fetch selector with the DeepSeek fetch backend), 14 (Remote magpie search pass-through).

**Status:** resolved

- [x] Write `TestRemoteMagpieFetchForwardsArgs` first and watch it fail, against a fake remote magpie: the picker offers each eligible remote; the call forwards `url`, `max_chars` and `offset` with the provider key; the answer text returns as-is.
- [x] `TestRemoteMagpieFetchFallbacks`: a transport failure or timeout falls back to magpie's own downloader; a remote tool error result surfaces its text without a fallback.
- [x] `TestRemoteMagpieFetchRow`: a successful pass-through records a zero-cost `web_fetch` row naming the caller and the remote provider.
- [x] A gone, off or keyless remote shows the “isn't used” warning and uses magpie's own downloader.

## Done

Merged as `817a17bc` on `deepseek-search-fetch`. Four tests red→green at a fake remote (negative control in `logs/15.log`); the GUI settings test extended to cover remote choices. Full gateway suite ok 364s; targeted gui tests ok; vet clean. Deviations recorded for review: no local private-URL check before forwarding (the remote's own downloader enforces public-only and its refusal surfaces); no JS change — `renderFetcher` renders whatever `fetchChoices` carries, so only the settings JSON changed. Wave-5 gate: VET=0 TEST=0 (`logs/gate-15.log`).

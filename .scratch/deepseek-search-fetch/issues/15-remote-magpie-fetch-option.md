# 15: Remote magpie fetch option

**What to build:** The “Fetch pages with” picker also offers each keyed Remote magpie. Choosing one forwards `url`, `max_chars` and `offset` to the remote's `web_fetch` tool; the remote applies them and its answer text returns as-is. A transport failure or timeout falls back to magpie's own downloader; a remote tool error surfaces. A zero-cost usage row records the pass-through.

**Blocked by:** 13 (Fetch selector with the DeepSeek fetch backend), 14 (Remote magpie search pass-through).

**Status:** ready-for-agent

- [ ] Write `TestRemoteMagpieFetchForwardsArgs` first and watch it fail, against a fake remote magpie: the picker offers each eligible remote; the call forwards `url`, `max_chars` and `offset` with the provider key; the answer text returns as-is.
- [ ] `TestRemoteMagpieFetchFallbacks`: a transport failure or timeout falls back to magpie's own downloader; a remote tool error result surfaces its text without a fallback.
- [ ] `TestRemoteMagpieFetchRow`: a successful pass-through records a zero-cost `web_fetch` row naming the caller and the remote provider.
- [ ] A gone, off or keyless remote shows the “isn't used” warning and uses magpie's own downloader.

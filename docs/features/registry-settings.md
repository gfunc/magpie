# Registry settings

The feature map for the Model registry section (specs:
[`docs/specs/registry-settings.md`](../specs/registry-settings.md), the
living spec, as amended by
[`docs/specs/registry-wires.md`](../specs/registry-wires.md), the
delta). Each entry names a changed user-facing behavior, the surface it
shows on, and the final-gate lane that drives it. The ids are stable —
`<feature-id>#<sub-id>` — and the same ids name the PR's live lanes, so
a lane drives the behavior it names.

| Entry | Behavior | Surface | Final-gate lane |
| --- | --- | --- | --- |
| `registry#section` | The section draws whole — registry Off/On selector, URL (greyed while off, its copy button always beside it), effort, four wire ticks, the groups selector under them, sync — with every new and changed string in en, zh, zh-TW, ja and de. | Settings › Network and sharing › Model registry | Lane 1: the live Settings draw |
| `registry#wires-gemini` | The Wires row carries four wires — OpenAI Chat, Anthropic Messages, OpenAI Responses, and the new Google GenAI wire (`magpie-gemini`, typed `google-genai`, api base + `/v1beta`) — an unchecked wire left out of the registry, its membership exactly the models served on generateContent untranslated. | Settings › Network and sharing › Model registry; `GET /api.json`; `magpie settings set registryBuckets …` | Lane 2: the live `curl /api.json` with a wire unchecked |
| `registry#groups-switch` | The routing groups ride their own entry by their own switch (default on) instead of a pseudo-protocol tick: "List routing groups" sits directly under Wires as an Off/On selector, and the groups entry is present or absent from the registry under it. | Settings › Network and sharing › Model registry; `GET /api.json`; `magpie settings set registryGroups false` | Lane 3: the live switch flip re-read against `GET /api.json` |
| `registry#empty-refusal` | A save that would list nothing — no wire and no groups — is refused `registry_empty`, the row's own line naming both levers (leave a wire on, or list routing groups), in-row with no toast in the window and as the CLI save's error; groups-only (every wire off, groups on) saves clean. | Settings › Network and sharing › Model registry; `magpie settings set`; the negative control | Lane 4: the scoped negative control on the refusal |
| `registry#migration` | A stored pre-split bucket list migrates once at load: its group tick (`magpie-group`) becomes the groups switch on with the id stripped, a list without it becomes groups off recorded explicitly — idempotent, and the legacy id is refused by saves after. | `magpie settings` over an upgraded config; the settings load path | Lane 5: the live upgraded-config load and re-save |
| `registry#sync-ride` | `registryGroups` rides the registry sync field group on every site — stripped while the per-machine switch is off, carried whole (or not at all: nil, never false) while on, landing only where both switches are on. | `magpie sync` over a synced fleet | Lane 6: the live two-machine sync round trip |
| `registry#appearance` | The section's polish: the copy button sits beside the URL unconditionally, all three switches (registry, groups, sync) draw as the horizontal Off/On selectors the rest of Settings uses, and the URL row's hint is client-neutral — a models.dev-shaped custom registry any able client can import, kimi-code's /provider one example. | Settings › Network and sharing › Model registry; the registry cjs lane | Lane 7: the registry Playwright lane, both engines, off state and all five languages included |

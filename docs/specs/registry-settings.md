# Registry settings section

Status: implemented. Adds a Settings section that governs
the gateway's api.json registry endpoint — the endpoint's on/off, the
default thinking effort every able model offers, which protocol wires the
registry carries models on, and whether it lists its routing groups —
replacing compose-file env knob-twiddling with GUI rows backed by settings
storage and CLI parity. Reshaped 2026-11-09 by the registry-wires change
([registry-wires.md](registry-wires.md)): the Protocol buckets row became
a Wires row (four entries, Google GenAI added) plus a List routing groups
switch, storage gained `registryGroups`, and the emptiness refusal became
`registry_empty`.

Acceptance fleet: both Docker stacks (dev 10.2.1.180, bi-dev 10.2.1.21)
plus the laptop.

## What exists today

- `GET /api.json` is served while `registryOn` is on (`s.apiJSON`,
  [`internal/gateway/gateway.go:956`](../../internal/gateway/gateway.go)):
  four wires (magpie-openai, magpie-anthropic,
  magpie-openai-responses, magpie-gemini) plus the routing-groups
  entry (magpie-group) while its switch is on, model set mirrors
  `/v1/models` (the Gemini wire: the models the gateway relays on
  generateContent untranslated), api base from `MAGPIE_PUBLIC_URL` else
  the request Host. The full shape was specced in the shipped api.json
  effort (`wayfinder/map.md`, tickets 01–05) and reshaped by
  [registry-wires.md](registry-wires.md).
- The only knob is `MAGPIE_REGISTRY_DEFAULT_EFFORT`
  ([`internal/gateway/apijson.go:19`](../../internal/gateway/apijson.go)):
  a level (`max`, say) offered as every able model's `default_effort`,
  applied only where the model's `Efforts` contains it, `""` offers none.
- Settings writes flow through `settingsapi.Apply`
  ([`internal/settingsapi/settingsapi.go`](../../internal/settingsapi/settingsapi.go))
  with GUI↔CLI parity: `settingsapi.Keys` names every saved field
  (`TestKeysAreWhatTheSaveWrites`), and the page's `savePrefs` keys are
  held to the same set (`TestSettingsCLISetsWhatTheWindowSaves`).
- Settings › Network and sharing draws network-facing rows (port, LAN
  sharing, browser access) via the gateway plugin's `settings.js`, run
  after app.js and before start.js.
- The Settings page hosts per-part drawing: plugins wrap `renderRedact`-
  style hooks or draw their own sections; the search APIs' rows live in
  `plugins/search/settings.js`. A fork-added section follows the same
  pattern.
- Thinking-effort vocabulary: the catalog's reasoning models carry
  `Efforts` (per-model levels, e.g. low/medium/high/xhigh/max;
  `internal/provider/groupeffort.go` `MemberEfforts` adds `none`).
  kimi-code starts a model on `default_effort` when the registry offers
  one, else the middle of `support_efforts`, else on. Effort shaping at
  request time is out of scope (see Non-goals).

## The section

One part in Settings › Network and sharing, after the Gateway rows, named
**Model registry**. Four rows:

1. **Registry switch.** On = the endpoint serves; off = `GET /api.json`
   answers 404. The row's line says the consequence plainly: models
   already imported by kimi-code keep working until its next successful
   refresh; a client that never refreshes keeps its imported models
   indefinitely. (kimi-code's docs document removal-on-successful-refresh
   — "upstream provider additions, removals, and model metadata changes
   are synced" — but not the fetch-failure path; a 404 leaves the stored
   config (`source.apiKey`, URL) intact so a later refresh can recover.
   Observed behavior, documented as such in the row's help text.)
2. **Default thinking effort.** A picker over five levels: low, medium,
   high, xhigh, max — plus a leading **Default (model's middle)** option
   that is the initial state and clears the setting. A model not
   supporting the chosen level falls back to no `default_effort` (its
   middle), exactly as the env var applies today (`apiJSONModel`, the
   `slices.Contains(e.Efforts, de)` guard).
3. **Wires.** Four ticks: OpenAI Chat, Anthropic Messages, OpenAI
   Responses, Google GenAI. Unchecked wires are omitted from the
   registry's providers object entirely; a client refresh then drops
   them. Default: all four on. Every wire off is legal while the groups
   switch is on (groups-only: a client imports exactly the curated set).
   The Google GenAI wire is the registry's `google-genai` entry (its api
   base + `/v1beta`), carrying the models the gateway relays on
   generateContent untranslated.
4. **List routing groups.** A switch, default on — the horizontal On/Off
   selector, as the section's other switches. The routing groups ride
   their own OpenAI-compatible entry, keeping its `magpie-group` id and
   routing-group-only membership; the switch alone gates it, not the
   Wires row. A save leaving no wire AND the groups off is refused (the
   registry would list nothing; `ErrRegistryEmpty`, code
   `registry_empty`), the row says why in the page's language.
5. **Sync across machines.** A switch, default off (per-machine,
   `KeepOwn` semantics: the registry fields are zeroed before settings
   sync/backup, `davsync`'s `KeepOwn` path). Flipped on = the registry
   fields ride settings sync like ordinary fields. The row's line says:
   per-machine keeps each box's registry independent; syncing couples the
   boxes' registry configs through settings sync.

While off, the section shows the registry URL greyed under the switch
(the add-to-a-client step stays visible): `MAGPIE_PUBLIC_URL` when set
(the Docker stacks' case), else the request's Host — the same rule the
endpoint itself uses. The copy button sits beside the URL unconditionally
— while off too, so a reader can stage a client before turning the
registry on — and the URL row's hint names no one client: a
models.dev-shaped custom registry any able client can import, kimi-code's
`/provider` one way to add it.

## Storage and precedence

Five flat fields on `settings.Settings`, each its own JSON name at the
top level of settings.json — the same shape every other settingsapi key
has:

```json
{
  "registryOn": false,
  "registryEffort": "",
  "registryBuckets": ["magpie-openai", "magpie-anthropic", "magpie-openai-responses", "magpie-gemini"],
  "registryGroups": true,
  "registrySync": false
}
```

`registryBuckets` holds wire ids only (`settings.RegistryWireIDs`); the
routing groups' `magpie-group` id is not among them — their own field is
`registryGroups` (`*bool`; absent means on, the same absent-means-default
read the bucket list has, so the zero value never silently turns the
groups off), and a stored list carrying the legacy id is migrated at load
(see below). The key is written without `omitempty`, so a groups-only
save stores `"registryBuckets": []` rather than dropping the key (a
dropped key would read back as all four wires on).

Default (fields absent): on = false, effort = "", buckets = all four
wires, groups = on, sync = false. **On defaults to false** — a deliberate
break from today's always-on endpoint; the two running stacks flip it via
CLI at upgrade (`magpie settings set registryOn true`), or by switching
the GUI row, and the row's line notes the upgrade path.

**Migration.** A stored settings file from before the split is mapped at
load (`settings.MigrateRegistry`, one site at the top of `normal()`, so
file parse, save and restore landing all run it): a bucket list
containing the group id becomes groups-on with the id stripped (a
`["magpie-group"]` list becomes groups-only, legal); one without it
becomes groups-off recorded explicitly; the list reads wires-only either
way. Idempotent — a migrated file matches neither branch.

Precedence, settings over env: the settings value wins when set; the env
var `MAGPIE_REGISTRY_DEFAULT_EFFORT` remains the fallback when
settings.json is absent or the effort field is empty. So the Docker
stacks' compose knob keeps working unchanged, and the GUI value applies
on top. Endpoint on/off has no env counterpart and needs none: off =
settings-only, default false.

Precedence for buckets: settings list wins when present; env has no
bucket counterpart. An empty list is a stored state (groups-only), not a
spelling of the default; the refusal below is the only guard, not a
normalization.

## Gateway behavior

- On: the endpoint serves as specced in the api.json effort, reshaped by
  [registry-wires.md](registry-wires.md). Wires in the settings list are
  emitted; unchecked wires are left out of the providers object, and the
  groups entry rides its own switch.
- Off: `s.apiJSON` answers 404 with a one-line JSON body naming the
    switch: `{"error": "registry disabled"}`. No other route changes.
- Effort: `registryDefaultEffort()` reads settings first (`settings.Load()`
  in the gateway's cached read path), else the env var, else "".
- The base URL rule is unchanged: `MAGPIE_PUBLIC_URL` else request Host
  — no new env vars.

## CLI parity

The settings save takes flat JSON names matching the settings struct's
fields — the `settingsapi.Keys` convention (`visionEffort`, not
`vision.effort`) — so the section's fields land as `registryOn`,
`registryEffort`, `registryBuckets`, `registryGroups`, `registrySync` in
both `settingsapi.Keys` and `settings.Settings`, and
`magpie settings list|get|set|reset` covers them like any other key. The
two stacks' upgrade path: `magpie settings set registryOn true` per
stack. Parity tests extend: `TestKeysAreWhatTheSaveWrites`,
`TestSettingsCLISetsWhatTheWindowSaves`,
`TestEveryRouteHasACommandOrIsNamed` when new routes appear.

## Acceptance criteria

1. Fresh settings.json → the section reads: switch off, effort Default
   (model's middle), all four wires checked, groups on, sync off; `GET
   /api.json` answers 404.
2. Toggle on → 200 with all four wire entries and the groups entry,
   the established importer shape per entry (id/name/api/type/models),
   the Google GenAI entry typed `google-genai`, api base + `/v1beta`,
   carrying the models the gateway relays on generateContent
   untranslated.
3. Set effort max → only models whose `Efforts` contain max carry
   `default_effort: max`; others carry none.
4. Uncheck a wire → omitted from the providers object; a kimi-code
   refresh drops it (verified on the laptop against a stack).
4b. Uncheck every wire with the groups on → legal (groups-only): the
   registry carries the groups entry alone. Uncheck every wire and turn
   the groups off → the save is refused (`registry_empty`) with a
   row-level reason in the page's language.
4c. Turn the groups switch off → the groups entry is omitted while the
   wires still serve.
5. Off → 404; models already imported by kimi-code keep answering (the
   request path `/v1/*` is untouched).
6. Env `MAGPIE_REGISTRY_DEFAULT_EFFORT=max` with settings effort unset →
   env applies (Docker stack behavior unchanged).
7. Sync switch off → the registry fields are absent from a davsync
   bundle's synced settings (KeepOwn zeroing verified in
   `davsync`'s hash path); on → present, and the whole config rides:
   the landing takes the incoming fields only when the bundle carried a
   whole config (wires and groups both) and both machines' switches are
   on — a peer still on the old shape (no groups field) never rides, so
   each machine keeps its own registry until the peer upgrades.
7b. Copy button → clipboard holds the resolved URL
   (`MAGPIE_PUBLIC_URL` else Host) + `/api.json`; present while the
   registry is off too.
8. Both stacks after upgrade + `magpie settings set registryOn true`:
   endpoint on via CLI alone, no GUI touch needed.
8b. `magpie settings set` refuses the all-off save (no wire and no
   groups) with the same `registry_empty` refusal (CLI parity for the
   GUI refusal); `registryGroups` and `registryBuckets` are CLI keys
   like the others (set/get/reset, absent-groups reads as on, reset
   restores all four wires / groups on).
8c. The row's lines exist in zh, zh-TW, ja, de (`gui-*.test.cjs` language
   tests fail on a missing one).
8d. A pre-split settings file loads migrated (group tick → groups on,
   id stripped; no group tick → groups off), and the first save of a
   fresh install leaves the groups on.

## Non-goals

- Per-model or per-wire effort overrides — the fog the first map left;
  graduate only if the global knob proves insufficient.
- Effort shaping at request time (forcing an effort on requests that
  don't ask for one) — that's routing behavior, not registry metadata.
- Extra registry features (fields beyond the importer's schema, a second
  wire beyond the four, new auth) — the importer's own shape is fixed.
- The sync description's whole-state rule and the migration's invariants
  are specced in [registry-wires.md](registry-wires.md); this page keeps
  the section's living shape only.
- Serving other agents' tooling — no second consumer is being designed
  for.
- The implementation itself: fork patch, image build, stack deployment,
  verification — a separately-charted effort.

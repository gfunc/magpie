# Registry listing reshaped — wires row with Gemini, and the routing-groups switch

Status: implemented (2026-11-09, PR registry-wires). Amends
[registry-settings.md](registry-settings.md), the Model registry section's
living spec, to the reshaped shape filed here; this page is the delta-spec
of that change, kept beside it. Charted from the wayfinder map
`wayfinder/registry-wires/` (tickets 01, 02, 04 resolved; 03 was the
landing).

## Problem Statement

The Model registry section's "Protocol buckets" row mixes two kinds of
thing: three ticks are protocol wires, while the fourth ("Routing groups")
is a model-set choice riding the OpenAI wire — a pseudo-protocol, which
reads as odd and hides that groups-only is already a choice. The registry
also lacks the Gemini generateContent wire even though the gateway serves
it untranslated, so clients that speak Google's API natively import through
a translation instead. And three GUI rough edges: the registry URL's copy
button appears only while the registry is on, the section's switches are
not the horizontal On/Off selectors every other Settings row uses, and the
URL row's hint addresses kimi-code alone — but the registry serves any
able client.

## Solution

Split the row: a **Wires** row — OpenAI Chat, Anthropic Messages, OpenAI
Responses, and a new **Google GenAI** wire — plus a separate **List
routing groups** switch (default on). The Gemini wire joins the registry
as a fourth provider entry typed `google-genai`, carrying exactly the
models the gateway relays on generateContent untranslated. The section's
GUI is polished per the owner: the copy button always beside the registry
URL, every switch drawn as the horizontal On/Off selector, and
client-neutral wording. Storage gains a `registryGroups` field (absent =
on) with a one-time migration of the old bucket list, refusal only when
no wire AND no groups would be listed, CLI parity for all of it, and sync
carrying the new field with the other registry settings.

## User Stories

1. As a registry owner, I want the wire choices separated from the
   routing-groups listing, so each tick in the row means one kind of thing.
2. As a registry owner, I want a fourth wire — Google GenAI — in the row,
   so clients that speak generateContent import natively instead of
   through a translation.
3. As a Gemini CLI or google-genai SDK user, I want the registry to offer
   a `google-genai` provider entry, so my client imports models it can
   call as-is.
4. As a registry owner, I want an explicit "List routing groups" switch,
   so the curated group surface is a first-class choice rather than a
   pseudo-protocol tick.
5. As a registry owner, I want groups-only to remain reachable (every
   wire off, groups on), so a client can import exactly my curated set.
6. As a registry owner, I want a save that would list nothing — no wire
   and no groups — refused with a reason in the row, so the registry is
   never stored empty by accident.
7. As an upgrading user with saved registry settings, I want my old
   bucket list migrated — its group tick becoming the new switch's state —
   so my configuration means the same after the upgrade.
8. As a synced-fleet operator, I want the groups switch to ride the same
   registry sync as the other registry fields, so the config lands exactly
   where the sync switch is on.
9. As a CLI-only operator, I want the new field and the emptiness refusal
   over `magpie settings` too, so a stack configures without the GUI.
10. As a registry reader, I want the copy button always beside the
    registry URL, so I can copy it even while the registry is off.
11. As a registry reader, I want the section's switches drawn as the
    horizontal On/Off selectors used elsewhere in Settings, so the page
    reads consistently.
12. As a user of any registry-able client — not only kimi-code — I want
    the URL row's hint to speak of custom registries generally, so I know
    the URL serves my client too.
13. As a registry owner, I want the Gemini wire's membership to be
    exactly the models the gateway relays on generateContent
    untranslated, so imported models behave as listed.
14. As an importer of the registry, I want each wire's entry to keep its
    established shape — id, name, api, type, models — so an existing
    import refreshes unchanged.
15. As a registry owner, I want routing groups to keep riding the OpenAI
    entry alone with their bare ids, so imported clients see the group
    surface exactly as before.
16. As a consumer of the model listings (/v1/models, an agent plugin's
    connect), I want the native-endpoint listing to name the true Gemini
    paths, so native Gemini models stop being under-reported there.
17. As a non-English reader, I want the new rows, switch, and refusal in
    zh, zh-TW, ja and de, so the section reads in my language.
18. As a registry owner, I want a fresh magpie to default to every wire
    on and groups on, so the registry serves everything until I narrow it.

## Implementation Decisions

- **Section shape.** The buckets row splits into a Wires row (OpenAI
  Chat, Anthropic Messages, OpenAI Responses, Google GenAI — each an
  unchecked-omitted provider entry, as today) and a "List routing groups"
  switch, default on. The registry switch, effort row, and sync switch
  are otherwise unchanged.
- **Storage.** `registryBuckets` holds wire ids only. A new
  `registryGroups` field joins the registry settings; absent means on —
  the same absent-means-default read the bucket list already has, so the
  Go zero value never silently turns groups off. Both are settingsapi
  keys with GUI↔CLI parity like every registry field.
- **Refusal.** A save leaving no wire checked AND groups off is refused —
  the registry would be empty — with one error code in the established
  row-reason pattern, surfaced as the row's own line in the GUI and as
  the CLI save's refusal. Groups-only (all wires off, groups on) stays
  legal.
- **Migration.** At load, a stored bucket list in the old shape is
  mapped: one containing the group id becomes groups-on with the id
  stripped; one without it becomes groups-off recorded explicitly; the
  list reads wires-only either way.
- **Gemini emission.** A fourth bucket: id `magpie-gemini`, name
  "Magpie (Google GenAI)", type `google-genai` — the importer's own type
  string, notably not `gemini` — and api = the resolved base + `/v1beta`
  (the Gemini base as Google's SDKs take it). Membership = models whose
  provider serves them natively on generateContent, derived the same way
  the other wires' membership is: from the native-endpoints table, which
  learns the Gemini protocol's per-model path (`/v1beta/models/{model}:
  generateContent`). The bucket match grows the per-model form. The
  native-endpoint listing's other consumers (/v1/models, an agent
  plugin's connect) gain the true paths as a deliberate, correct side
  effect.
- **Groups emission.** The routing-groups entry keeps its id, its bare
  model ids, and the OpenAI wire, gated by the new switch instead of the
  bucket list.
- **Sync.** `registryGroups` joins the registry field group on every sync
  site — the backup strip, the sync-load fallback, the blind copy, and
  the merge/landing rule — gated by the registry sync switch like the
  rest.
- **GUI.** The copy button sits beside the registry URL unconditionally
  (the same copy component the section already uses). All three of the
  section's switches — the registry switch, sync, and the new groups
  switch — draw as the horizontal On/Off segmented selector the rest of
  Settings uses. The URL row's hint becomes client-neutral: a
  models.dev-shaped custom registry any able client can import, with
  kimi-code named as one example, not the addressee. The section's five
  languages carry every new and changed string.
- **Docs.** The landing amends the section's living spec
  (registry-settings) to the reshaped shape, files this delta-spec beside
  it, and updates the section's feature doc.

## Testing Decisions

A good test here drives external behavior at a seam that already exists —
the HTTP endpoint, the settings save surface, the Settings page, the sync
path — and never the emission internals. No new seams; four existing
ones:

1. **The api.json endpoint** (the gateway's endpoint tests, prior art the
   registry's own suite): emission with every wire and groups on
   (defaults), the Gemini entry's shape and its exact membership, the
   groups entry's presence/absence under the switch, a wire's omission
   when unchecked, and the importer-facing shape (id/name/api/type/
   models) asserted field-for-field.
2. **The settings save surface** (the keys-parity and CLI-parity suites):
   the new key in both surfaces, the emptiness refusal on both sides,
   and the absent-means-on default read.
3. **The Settings page** (the section's existing Playwright page test,
   both engines, faked API): the Wires row's four ticks and their posts,
   the groups switch and its post, the always-present copy button (off
   state included), the On/Off selectors' rendering and aria state, the
   refusal line in the row, and the new strings in all five languages.
4. **The sync path** (the backup and sync suites): the groups field
   stripped while per-machine, carried while synced, and the landing rule
   holding with it.

Negative controls: the all-off refusal (GUI row reason and CLI error
both), the Gemini entry absent when its wire is unchecked, the groups
entry absent when the switch is off, and both migration directions
(group id present → on; absent → off) read through the public load path.

## Out of Scope

- Per-model or per-provider listing curation (the picker option) — a
  separately-charted effort if entry-level control proves insufficient.
- Carrying routing groups on the Gemini wire — groups ride the OpenAI
  entry alone.
- The effort picker's "model's middle" wording — parked on the map; it
  may ride the landing ticket if the owner asks.
- Request-time behavior: translation, effort shaping, serving of
  unchecked wires (unchanged — a tick shapes the listing, never the
  wire).
- Registry schema beyond the importer's own (no new fields, no second
  consumer, no auth).
- Offering anything to yetone/magpie: the repository takes no pull
  requests.

## Further Notes

- The map `wayfinder/registry-wires/` holds the grounding: the gateway
  already serves the Gemini wire untranslated; the importer names the
  type `google-genai` in its known-wire list; the native-endpoints table
  is the one gap to close.
- The fleet has not yet deployed the registry section at all (off by
  default until each stack flips it); this reshape lands before that
  deploy, so the migration matters chiefly for the laptop's saved
  settings.
- One spec, one landing: the whole delta is one PR on fork main, per the
  fork's patched-main workflow.

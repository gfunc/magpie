# Synthesized design v2 — subagent effort follows the Agents-page effort

Model: synthesis (base A + grafts from B, design-review fixes applied)

## Problem

Pool subagents bind each model table's declared `default_effort` before kimi-code's global `[thinking].effort`; the main agent is the reverse. Our writer (`internal/agent/kimi.go` `kimiModelTables` → `kimiEfforts`) sets `default_effort = vendor default, else high`, so GLM/MiniMax pool subagents run `high` while the user's Agents-page effort is `max`. Section-wide `[secondary_model].default_effort = "max"` is invalid on mixed pools (grok/qwen lack max; startup validation fails). Constraints: unset effort → unchanged behavior; hand-set values preserved; legacy-high tables migrate on next sync; no `/api.json`, schema, or UI changes.

## Usage (caller's view)

User picks Agents → Kimi Code → thinking → max. While wired, magpie writes every magpie model table's declared default from that one value: glm/minimax high → max, gpt-6.1-sol ultra → max, grok/qwen stay xhigh (their highest). Picking default ("") restores the vendor-default/high fallback for magpie-owned entries. A hand-set `default_effort` stays the user's while it is a level of the model. Main-agent runtime is unaffected (kimi-code's global overrides a declared default there).

Two limits the user can see:
- A section-wide `[secondary_model].default_effort`, when set, overrides every per-table default (kimi-code's resolution order). magpie leaves that key alone and shows a Notice naming it, so the user knows the per-table defaults are inert until it is removed or aligned. On dev it is unset; on local it is `max`, valid for every local pool model.
- An effort pick while `default_model` is the user's own (not magpie's) does not refresh immediately; the next Sync covers it.

## Shape

### Signatures (final)

```go
// the whole selection rule, pure. Chain: a non-empty want if offered, else the model's
// highest by catalog.EffortRank, else vendor if offered, else high if offered,
// else "". "none" is never a level; an unrankable list falls through to the
// vendor/high chain exactly as today, so no existing default is ever dropped.
func kimiDefaultEffort(want, vendor string, efforts []string) string

// levels' strongest by catalog.EffortRank/EffortLevel, "" when nothing ranks.
func kimiHighestEffort(levels []string) string

// vendor param replaced by prefer (the pre-resolved pick from kimiDefaultEffort).
// support_efforts quoting + kimiOwn preservation stay here.
// A prefer outside levels is dropped to the vendor-invalid path (high if
// offered, else no default) — never written: an out-of-levels prefer can only
// come from a direct caller, not from kimiDefaultEffort.
func kimiEfforts(efforts []string, prefer, was string, wrote map[string]string) (kvs []edit.KV, def string)

// preferred is the Agents-page effort, threaded from writeMagpie.
func kimiModelTables(path string, legacy bool, preferred string, wrote map[string]map[string]string) ([]edit.Table, map[string]map[string]string)

// one location, one reader for the [thinking] table's effort key.
func kimiThinkingEffort(path string) (string, error)
```

Per model in `kimiModelTables`:

```go
effs, def := kimiEfforts(m.Efforts,
    kimiDefaultEffort(preferred, m.DefaultEffort, m.Efforts),
    was["default_effort"], wrote[name])
```

`kimiThinkingEffort` reads `[thinking]` once with `edit.GetTOMLTable(path, "thinking")` and returns `t["effort"]` plus the read error. The effort field's `Get` uses it too (display keeps ignoring the error as today).

`writeMagpie` (modern only) calls it once. On a read/type error (e.g. an array table `[[thinking]]`, valid TOML that syncs fine today): **degrade to the pre-change behavior** — write with `preferred = ""` (vendor/high fallback, exactly today's output) and set a Notice naming the unreadable `[thinking]` table. It must not abort the takeover: aborting would strip magpie's models from a config that works today. Unknown degrades to the old behavior with a warning, never to a write of new policy.

The effort field's `Set`, after a valid set/removal, calls `writeMagpie()` only when `usesMagpie(get())` (the file's existing gate idiom). `writeMagpie`'s stash write of the `wrote` record is made **error-returning**: if it fails, `writeMagpie` returns the error so `atomic(a, path)` rolls back config.toml too. Config file and ownership record now succeed or fail together; a record lost after a successful write would otherwise make the next sync read magpie's own value as hand-set and stop following.

Disconnect ordering precondition (documented + pinned by test): `Disconnect` clears the model field before the effort field, so `usesMagpie(get())` is already false when the effort field's `Set("")` runs and no magpie tables are recreated.

### Selection and ownership

`kimiDefaultEffort` holds the only rule:
- want != "" and in levels (minus none) → want.
- want != "" and not in levels → `kimiHighestEffort(levels)` if anything ranks; **otherwise fall through to the vendor/high chain** (no default is ever removed that today's rule would write).
- want == "" → vendor if offered, else "high" if offered, else "".

`kimiEfforts` keeps output formatting and the `kimiOwn` bookkeeping: hand-set `was` (≠ wrote) wins while in levels; a magpie-owned old value (`was == wrote`) moves to the new pick; unrecorded legacy `high` is re-owned and migrates on next sync. The not-in-levels guard (`v, by = prefer, prefer != ""`) is replaced by: an out-of-levels `v` from a direct caller falls back to high-if-offered, else no default — so no emitted `default_effort` can ever sit outside its table's `support_efforts`.

Existing direct call sites (`kimi_caps_test.go:153,157,161`) keep their expectations: 153/157 pass an in-levels prefer (unchanged); 161 passes out-of-levels `"max"` and still gets `"high"` via the vendor-invalid path.

## Synthesis decision

Base **A** (uniform application) with two grafts from B (pure selection seam; rank-based clamp with vendor/high fallthrough), then the design review's fixes: the out-of-levels guard, the degrade-on-read-error policy, the error-returning stash write, the documented Disconnect precondition, the section-wide-key Notice, the doc update, and the single effort reader.

Cross-judge verdict: A (27 vs 26) — exact contract fit, no pool-coupling failure modes; B's blast-radius advantage is cosmetic (main-agent runtime is governed by the global either way).

## Tradeoffs accepted

- Full magpie model-table rewrite on each wired effort pick, in exchange for immediately correct derived defaults.
- Declared defaults change in tables also usable by the main agent (display churn only; runtime is governed by the global).
- Below-floor wants clamp to the model's highest when anything ranks; below-floor with nothing rankable falls to vendor/high (today's behavior).
- On a `[thinking]` read error the write degrades to pre-change behavior with a Notice, deliberately trading "unknown is not unset" for not stripping a working takeover.
- The legacy-high heuristic's known ambiguity is retained.

## Alternatives considered

- **Pool-scoped (B)**: applies only to `[secondary_model.models]` members. Lost on contract fit and on coupling the writer to kimi-code's pool shape for no runtime gain outside the pool; its grafts are taken.
- **Section-wide `[secondary_model].default_effort`**: invalid on mixed pools (startup validation).
- **Nearest-effort clamp**: below-floor wants would weaken instead of reaching the model's highest.

## Formal signal

`state machine/protocol` — kimi-code's config resolution order (section-wide key → declared default → global → middle) and its startup validation. Property: **every emitted `default_effort` is a member of its table's `support_efforts`, and a fresh subagent on a followed table binds that value in `profile.bind` and `llm.request`.** Verified on dev by a real spawn after the sync (the Go tests alone cannot prove kimi-code's runtime resolution).

## Test plan

Pure (no files), in `kimi_efforts_test.go`:
- `TestKimiDefaultEffort`: B's 12 rows (exact want, grok/qwen clamp, gpt want-beats-vendor, a no-default model gaining a default when a want is set, none-exclusion, unset fallbacks) + an unrankable-list case that falls to vendor/high instead of dropping the key + a below-floor clamp case.
- `TestKimiHighestEffort`: ranked order, none excluded, unrankable → "".

Composition, through the real agent path (`kimi(home)`, `a.Apply("model", ...)` so the gate is live):
- effort=max → glm high→max, grok/qwen xhigh, gpt ultra→max; effort="" → vendor/high restored; hand-set in-levels preserved; hand-set not-in-levels → replaced by the pick; unrecorded legacy high migrates to the pick; recorded high migrates too.
- pick while not wired (`default_model` the user's own) → model tables byte-identical, only `[thinking].effort` changes.
- Disconnect (model field cleared before effort) → no magpie tables recreated.
- failed refresh (stash write error) → `writeMagpie` returns the error and `atomic` rolls back `[thinking].effort` and the tables.
- unreadable `[thinking]` (array table) → takeover still writes provider/model tables with pre-change defaults + a Notice; nothing aborts.
- section-wide `[secondary_model].default_effort` present → per-table defaults still written, and the Notice names the section-wide key.
- the three `kimi_caps_test.go` call sites keep their expectations (161 out-of-levels → high).

Negative-control protocol: the integration test must fail on the pre-change code (`deep = high`, no `wide` default instead of `max`/`medium`), not merely fail to compile.

Docs: one-line update to `docs/subsystems/agent-wiring.md` (Agents-page-first rule, clamp, hand-edit ownership, the section-wide-key note) in the same commit, per `docs/subsystems/README.md`.

## Open questions and risks

- Real-kimi verification on dev after the sync (the formal property above).
- New effort vocabulary must rank in `catalog.EffortRank`; unrankable levels fall to vendor/high (no key dropped).

## Next implementation step

Write `TestKimiDefaultEffort` + `TestKimiHighestEffort` as red tests, then implement the rule, the `preferred` threading, the single reader, the degrade-on-error policy, the error-returning stash write, and the wired refresh in `internal/agent/kimi.go`, then the composition suites and the doc line.

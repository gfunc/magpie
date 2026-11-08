# Candidate B — pool-scoped `default_effort` following the Agents-page effort

Model: architect runners=magpie/auto/glm-5.3
Direction: pool-scoped application (owned, not hedged)
Files in play: `internal/agent/kimi.go`, `internal/agent/kimi_efforts_test.go`, `internal/agent/kimi_caps_test.go` (three call lines), one line in `docs/subsystems/agent-wiring.md`

## Problem

kimi-code resolves a pool subagent's effort from the bound model entry's declared `default_effort` *before* it ever looks at the global `[thinking].effort` (configSection.ts → thinking.ts), and the entries are ours: `kimiModelTables` writes `default_effort = vendor default, else high`. So the effort the user picks on magpie's Agents page (`[thinking].effort`) never reaches pool subagents — dev's glm/minimax run `high` while the user asked for `max`.

The section-wide `[secondary_model].default_effort = "max"` is off the table: kimi-code validates it against *every* pool alias's `support_efforts` at session start, and grok-4.7 / qwen3.8-max-0902 have no `max` — startup fails. So the fix must be per-table. Constraints from Phase A that the shape must honor: the effort picker writes `[thinking].effort` and nothing else (no Settings UI, no schema change, `/api.json` untouched); an unset effort must preserve today's behavior exactly; hand-set `default_effort` values keep their `kimiOwn` preservation; the magpie-written `[models."magpie/…"]` tables are exactly the keys the `[secondary_model.models]` pool aliases reference.

The scope decision is the whole design: **the Agents-page effort applies only to models whose key kimi-code's own `[secondary_model.models]` pool names.** The pool is kimi-code's declaration of which models run as subagents, and the resolution order that ignores the global applies only to those. Every other magpie table keeps the vendor-default/high rule. No new magpie state, no new setting — the scope is read from the user's own config at write time.

## Usage (caller's view)

README-style, what a magpie maintainer reads:

> When magpie takes over Kimi Code it writes one `[models."magpie/<provider>/<model>"]` table per catalog model. The `default_effort` in each follows the **Agents-page effort** (`[thinking].effort`) **for the models kimi-code's subagent pool names** in `[secondary_model.models]`: that effort when the model offers it, else the model's highest level. A grok in the pool asked for `max` gets `xhigh`; a gpt-6.1-sol in the pool moves `ultra → max`. Models not in the pool, no pool at all, or no effort set: unchanged — the vendor's own default, else `high`. A `default_effort` the user set by hand still wins while the model offers it. Picking the effort on the Agents page rewrites the tables at once while Kimi runs one of magpie's models.

The two real call sites (both already in `kimi.go`; neither changes signature):

```go
// writeMagpie, on every takeover write (Connect, model pick, Sync, rewire):
tables, wrote := kimiModelTables(path, legacy, kimiWrote(path)) // reads [thinking].effort + pool keys itself

// kimiModelTables' per-model body (the only composition point):
want := ""
if pool[magpieID+"/"+m.ID] { // pool read once per write from the same config.toml
    want = effort
}
effs, def := kimiEfforts(m.Efforts,
    kimiDefaultEffort(want, m.DefaultEffort, m.Efforts), // the whole rule, pure
    was["default_effort"], wrote[name])
```

And the test author's view — the rule is two pure functions, so the matrix is table-driven with no file at all:

```go
if got := kimiDefaultEffort("max", "", []string{"low", "medium", "high", "xhigh"}); got != "xhigh" { // grok clamps
if got := kimiDefaultEffort("max", "ultra", []string{"low", "high", "max", "ultra"}); got != "max" { // gpt: want beats vendor
if got := kimiDefaultEffort("", "", []string{"low", "medium"}); got != "" { // today's no-default stays
```

## Shape

Data flow: `[thinking].effort` + `[secondary_model.models]` keys are read once per takeover write from the same `config.toml` `kimiModelTables` already takes as a parameter, combined per model into `want`, and resolved to one preferred default by a single pure function; `kimiEfforts` keeps doing the only things it did that nothing else can — quoting `support_efforts` and running the `kimiOwn` preservation bookkeeping. `kimiModelTables`' signature does not change, so `writeMagpie`, `Sync`, the model field, WSL (same `kimiIn(place)` path), and every existing test keep calling it as-is.

### New and changed signatures in `internal/agent/kimi.go`

```go
// kimiThinkingEffort is the effort of Kimi Code's [thinking] table — the one
// magpie's Agents-page effort picker writes and Kimi Code's own main agent
// takes as its global: "" when none is set. The takeover's model tables
// follow it for the models kimi-code's subagent pool names (kimiSubagentPool).
func kimiThinkingEffort(path string) string {
	t, _ := edit.GetTOMLTable(path, "thinking")
	return t["effort"]
}

// kimiSubagentPool are the keys of Kimi Code's [secondary_model.models]
// table, the aliases its pool runs as subagents — each key a
// [models."<key>"] table's name, magpie's among them. Both spellings count:
// the table's own and dotted keys on [secondary_model]. An absent pool is an
// empty one: no model follows anything. The values (each alias's
// description) are the user's words, not magpie's to read; only the keys are.
func kimiSubagentPool(path string) map[string]bool {
	pool := map[string]bool{}
	if t, _ := edit.GetTOMLTable(path, "secondary_model.models"); t != nil {
		for k := range t {
			pool[k] = true
		}
	}
	if t, _ := edit.GetTOMLTable(path, "secondary_model"); t != nil {
		for k := range t {
			if key, ok := strings.CutPrefix(k, "models."); ok {
				pool[key] = true
			}
		}
	}
	return pool
}

// kimiDefaultEffort is the default effort magpie writes a model table with:
// want — the Agents-page effort, for a model the subagent pool names — when
// one is set: itself when the model offers it, else the model's highest
// level (a grok asked for max runs at its xhigh; "none" is no level). With
// no want, today's rule: vendor, the vendor's own default, when the model
// offers it, else high. "" is no default at all — Kimi Code starts the
// model in the middle of its levels.
func kimiDefaultEffort(want, vendor string, efforts []string) string {
	var levels []string
	for _, e := range efforts {
		if e != "none" {
			levels = append(levels, e)
		}
	}
	if want != "" {
		if slices.Contains(levels, want) {
			return want
		}
		return kimiHighestEffort(levels)
	}
	if slices.Contains(levels, vendor) {
		return vendor
	}
	if slices.Contains(levels, "high") {
		return "high"
	}
	return ""
}

// kimiHighestEffort is levels' strongest by catalog.EffortRank (a vendor's
// own word for a level placed by catalog.EffortLevel), "" when none ranks.
func kimiHighestEffort(levels []string) string {
	best, at := "", -1
	for _, l := range levels {
		if i := slices.Index(catalog.EffortRank, catalog.EffortLevel(l)); i > at {
			best, at = l, i
		}
	}
	return best
}
```

`kimiEfforts` — signature unchanged in arity, second parameter renamed and its rule moved out (per boundary-discipline: the selection rule is one pure function, `kimiEfforts` is the output boundary):

```go
// kimiEfforts are a thinking model's levels for the new Kimi Code's thinking
// picker (#333): support_efforts, and default_effort, as Kimi Code takes for
// its own models (else it starts on the middle one). none is left out: Kimi
// Code's own off turns thinking off. kimi-cli has neither key. prefer is the
// default magpie has chosen for the model — kimiDefaultEffort's pick, one of
// the model's levels or none at all; anything else a caller passes is
// dropped rather than written. was is the table's default_effort now and
// wrote what magpie wrote in the table last (kimiOwn): one set by hand stays
// while the model has it. def is magpie's own default, "" when the table
// keeps the user's.
func kimiEfforts(efforts []string, prefer, was string, wrote map[string]string) (kvs []edit.KV, def string) {
	var levels, quoted []string
	for _, e := range efforts {
		if e != "none" {
			levels = append(levels, e)
			quoted = append(quoted, strconv.Quote(e))
		}
	}
	if len(levels) == 0 {
		return nil, ""
	}
	kvs = []edit.KV{{Path: "support_efforts", Value: edit.Raw("[" + strings.Join(quoted, ", ") + "]")}}
	// the vendor-default-else-high rule lives in kimiDefaultEffort now; a
	// prefer outside the model's levels is a caller's bug, not a default
	if prefer != "" && !slices.Contains(levels, prefer) {
		prefer = ""
	}
	// high in a table magpie kept no record of is the one it wrote before
	// it kept one (it wrote high on every model that had it)
	if _, ok := wrote["default_effort"]; !ok && was == "high" {
		was = ""
	}
	v, by := kimiOwn(map[string]string{"default_effort": was}, wrote, "default_effort", prefer)
	if !by && !slices.Contains(levels, v) {
		v, by = prefer, prefer != ""
	}
	if v == "" {
		return kvs, ""
	}
	kvs = append(kvs, edit.KV{Path: "default_effort", Value: v})
	if by {
		def = v
	}
	return kvs, def
}
```

`kimiModelTables` — signature unchanged (`path string, legacy bool, wrote map[string]map[string]string`); new body at the top and in the effort block:

```go
	// the Agents-page effort, followed by the models kimi-code's subagent
	// pool names; read here, once per write, never stored (kimi-code 2.x
	// keys: legacy writes neither)
	effort, pool := kimiThinkingEffort(path), kimiSubagentPool(path)
	...
	// inside the !legacy block, replacing today's kimiEfforts call:
	want := ""
	if pool[magpieID+"/"+m.ID] {
		want = effort
	}
	effs, def := kimiEfforts(m.Efforts, kimiDefaultEffort(want, m.DefaultEffort, m.Efforts),
		was["default_effort"], wrote[name])
```

The effort field (`Field{Key: "effort"}`) — `Get` delegates to the shared reader; `Set` re-syncs the tables while the takeover is live, so a running session's subagents take the new default at once (Kimi Code 2.x reloads config.toml as it runs):

```go
	Get: func() string { return kimiThinkingEffort(path) },
	Set: func(v string) error {
		if v == "" {
			if err := edit.DelTOMLKey(path, "thinking", "effort"); err != nil {
				return err
			}
		} else {
			if !slices.Contains(levels, v) {
				return fmt.Errorf("%q is not one of Kimi Code's thinking levels", v)
			}
			if err := edit.SetTOMLKey(path, "thinking", "effort", v); err != nil {
				return err
			}
		}
		// the tables follow the effort for the pool's models: write them
		// again while kimi runs one of magpie's (a kimi that doesn't keeps
		// its own tables — an effort pick must not rewire it)
		if usesMagpie(get()) {
			return writeMagpie()
		}
		return nil
	},
```

The `atomic(a, path)` wrapper already makes each Set one all-or-nothing edit of config.toml, so a `writeMagpie` failure rolls the effort key back too.

### Where things are read

- **Agents-page effort:** `edit.GetTOMLTable(path, "thinking")["effort"]`, once per takeover write in `kimiModelTables`, through the same `kimiThinkingEffort` the field's `Get` uses — one function names where the effort lives, two callers (single source of truth for the location, per derive-don't-sync).
- **Pool keys:** `edit.GetTOMLTable(path, "secondary_model.models")` plus the dotted-key spelling folded in from `[secondary_model]` — once per write, membership only. Verified against the real pool on this machine (`[secondary_model.models]` own-table spelling, quoted keys: `"magpie/auto/glm-5.3-flash" = "描述"`) and against `edit`'s parser: quoted keys of both TOML quote styles come back decoded (`magpie/auto/glm-5.3-flash`), so they compare directly against `magpieID+"/"+m.ID`. A magpie model key can never be a bare TOML key (it contains `/`), so the quoted form is the only one to handle.

### Default-selection rule (the whole invariant list)

For each catalog model `m`, with `want = effort` if `pool["magpie/"+m.ID]` else `""`:

1. `want != ""` and in the model's levels (minus `none`) → that effort.
2. `want != ""` and not in levels → the model's **highest** level by `catalog.EffortRank` (grok/qwen: `xhigh`; gpt-6.1-sol: `max` is in its levels so rule 1 gives `max`, moving it off `ultra`). A level no vendor word ranks is never the highest; if nothing ranks, no default.
3. `want == ""` (unset effort, no pool, or model not in the pool) → exactly today's rule: vendor default if offered, else `high` if offered, else no default.

Hand-set values and migration, both carried by the unchanged `kimiOwn` machinery:

- A `default_effort` the user set by hand (`was` ≠ last `wrote` record) stays while it is one of the model's levels — it beats both the vendor default and the followed effort, pooled or not. When it stops being a level, it falls to magpie's pick.
- Migration of the legacy `high`: a table reading `high` with no wrote record is re-owned as magpie's old write and takes the new pick on the next sync. dev's glm/minimax tables move `high → max` whether their `high` is recorded in the stash (written by a current magpie: `was == wrote`, not hand-set, overwritten) or unrecorded (legacy re-own). Tables recorded as magpie's follow every later change of the effort or the pool on the next write, and `SetTOMLTables` replaces whole tables, so a default that should no longer exist disappears rather than lingering. All idempotent: the same config + stash produces the same tables on every write (make-operations-idempotent).

Structural guarantees worth naming (encoded in the flow, not checked at runtime):

- **No pool → current behavior everywhere**: empty map ⇒ `want == ""` for every model.
- **A pool alias with no matching table is left alone**: `kimiModelTables` writes tables for catalog models only and reads pool keys for membership only — it cannot create or edit a table for an alias like `kimi-code/k3` or a stale `magpie/…` key.
- **Legacy kimi-cli**: the `!legacy` block writes neither effort key, so the pool read is inert there.

Interface depth: the public surface (package-private, but the same judgment) *shrinks* in what callers must know. `kimiEfforts` loses the vendor/high rule; `kimiModelTables` absorbs the scoping decision behind an unchanged signature; the two reads hide behind `kimiThinkingEffort`/`kimiSubagentPool`. A caller — or a test — composes at exactly one place. The `prefer`-outside-levels drop in `kimiEfforts` is a defensive boundary check, not a second rule: the rule lives once, in `kimiDefaultEffort`.

## Synthesis decision

*To be filled by the arena.*

## Tradeoffs accepted

- We accept reading the user's kimi-code config as the scope — pool membership is whatever `[secondary_model.models]` says *at write time* — in exchange for zero new magpie state, no Settings UI, and no second list to keep in sync. Corollary: a pool edited after a magpie write is picked up on the next write (Sync, model pick, connect, effort pick), not instantly. No watcher; that table is kimi-code's.
- We accept that a want *below* a model's floor clamps to its **highest** level (the assigned rule), so a pooled model asked for `low` but offering only `medium…max` runs at `max` — never a silently weaker level — in exchange for one dead-simple rule that also does the right thing at the ceiling, where every real case lives (grok/qwen/gpt under `max`).
- We accept the effort field's `Set` growing a `writeMagpie` re-sync (gated on `usesMagpie(get())`) in exchange for the knob landing on the tables at once while connected; the gate keeps a disconnected kimi untouched, and `atomic` keeps a failed re-sync from leaving a half-applied effort.
- We accept that the inline-table pool spelling (`models = { … }` on `[secondary_model]`) is not read — `edit.GetTOMLTable` omits inline tables, kimi-code never writes that spelling, and parsing inline tables would need new `edit` surface for a hand-edit nobody has made.
- We accept the `none`-exclusion filter appearing in two small pure functions (`kimiDefaultEffort`, `kimiEfforts`) rather than a shared helper — each needs it for a different shape, and a shared one would couple a selection rule to an output formatter.
- We accept that non-pooled tables keep the vendor-default/high rule even while the effort is set. It looks like an inconsistency from one file; it is the point of the direction: for the main agent, kimi-code's global *overrides* the declared default, so following would buy nothing there while changing what the `/model` picker starts at for users who never pool anything.

## Alternatives considered

- **Uniform application** — every magpie table follows the Agents-page effort (the spec's original "apply to all" reading, likely candidate A's base). Lost on interface depth *and* blast radius: for main-agent tables the declared default is overridden by the global anyway, so following is a no-op in behavior but rewrites what the `/model` picker shows as the starting level for every model on every effort change; the bug being fixed lives only in the pool; and uniform needs its own answer to "which models are subagents" where pool-scoping reads the user's own declaration for free. It also cannot write the section-wide key (grok/qwen lack `max` — startup validation fails), so it is per-table churn for churn's sake.
- **`[secondary_model].default_effort` (section-wide)** — one key instead of N. Rejected by Phase A: kimi-code validates it against *every* pool alias's `supportEfforts` at session start; one grok in the pool and `max` kills startup. Not a shape, a trap.
- **`want` as a fifth parameter of `kimiEfforts`** — one function instead of two. Lost: five mostly-positional strings, an almost-always-empty param every caller and test must pass, and the scoping decision leaking past `kimiModelTables` into the leaf. The rule split I chose instead costs one renamed parameter and three updated test call lines.
- **Nearest-effort clamping** (`catalog.NearestEffort`) instead of highest. Lost per the assigned rule: nearest is the gateway's request-time philosophy, but here a pooled model without the asked level would get a *weaker* nearest level when the want sits below its floor; the direction says the model's highest.

## Open questions and risks

- Is clamping a *below-floor* want to the model's highest really wanted (pooled grok asked for `low` → `xhigh`)? The rule as assigned says yes; if not, the floor case alone could take `NearestEffort` while the ceiling keeps highest — one line in `kimiDefaultEffort`.
- The effort pick's re-sync gate is `usesMagpie(get())` — kimi's `default_model` is one of magpie's. A kimi whose `default_model` is the user's own while pool aliases name magpie models gets no prompt re-sync (next Sync covers it). Is that state real on dev?
- A user who pools a *stale* magpie key (model no longer in the catalog) gets no followed default for it — the alias is left alone by design. Confirm dev's pool keys all match current catalog ids (the local machine's pool does).
- `docs/subsystems/agent-wiring.md` (the Kimi Code model-table sentence) needs the pool-following clause — one line, part of the implementation commit, not this package.

## Unit tests (added to `internal/agent/kimi_efforts_test.go`; three existing call lines in `kimi_caps_test.go` move their rule through `kimiDefaultEffort`)

Pure rule, table-driven, no files — `TestKimiDefaultEffort`:

| want | vendor | levels | expect |
| --- | --- | --- | --- |
| max | — | none,low,high,max | max (exact) |
| max | — | low,medium,high,xhigh | xhigh (grok clamps) |
| max | — | low,medium,xhigh | xhigh (qwen clamps) |
| max | ultra | low,high,max,ultra | max (want beats vendor — gpt) |
| max | — | low,medium | medium (a no-default model gains one when pooled) |
| high | — | none,high | high |
| xhigh | — | none,low | low (none is never the highest) |
| "" | max | none,low,high,max | max (today) |
| "" | — | low,high | high (today) |
| "" | ultra | low,high,max | high (today's fallback) |
| "" | — | low,medium | "" (today: no default) |
| "" | — | none / empty | "" |

Pool reading — `TestKimiSubagentPool` (temp config.toml): own-table spelling with quoted keys returns the decoded keys; dotted spelling on `[secondary_model]` folds in; both quote styles decode; absent `[secondary_model]` → empty; values/descriptions ignored (return type is the set).

Composition, through the real field and Sync with a `kimiPoolConfig` fixture (kimiThinkingConfig + `[secondary_model.models]` naming some magpie keys) and a dev-like catalog (glm-ish vendor-less `high` fallback model, gpt-ish `ultra`-default model with `max` in levels, grok-ish `xhigh`-top model, one out-of-pool model) — `TestKimiEffortsFollowThePool`:

1. connected, effort=max, pool names glm/gpt/grok tables: glm `high → max`, gpt `ultra → max`, grok stays `xhigh`; the out-of-pool model keeps its vendor default.
2. effort set, **no** `[secondary_model.models]` at all → every table unchanged (the direction's no-pool rule).
3. pool names one table only → that one follows, the others unchanged.
4. effort unset, pool present → all unchanged.
5. hand-set `default_effort` on a pooled model preserved while in levels, across an effort change and a Sync; falls back when the level goes away.
6. migration: pooled table reading `high` with no wrote record → `max` on the next write (legacy re-own under following); recorded `high` → `max` too.
7. un-pooling (alias removed) or clearing the effort (`Pick("thinking","")`) → next Sync reverts the default to vendor/high.
8. a pool alias with no matching table (`"kimi-code/k3"`) → no table created or changed for it.
9. `Apply("thinking", "max")` while connected rewrites the pooled tables at once; while disconnected (default_model the user's own) it changes nothing but `[thinking].effort` — the existing `TestKimiThinkingEffortWhileDisconnected` must still pass unmodified.
10. legacy kimi-cli with a pool present → still neither effort key written.

Regression: `TestKimiEfforts` and `TestKimiEffortsSurvivesModelChanges` pass unchanged (their configs have no pool), which pins the no-pool behavior against drift.

## Next implementation step

Write `kimiDefaultEffort`, `kimiHighestEffort`, `kimiThinkingEffort`, and `kimiSubagentPool` with their tests first (pure, no files for the first two), then the six-line composition in `kimiModelTables`, then the effort field's `Get`/`Set` re-sync, then `go test ./internal/agent/ -run Kimi` and `go vet ./internal/agent/`.

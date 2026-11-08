# Candidate A: uniform Agents-page effort

Model: architect runners=magpie/kimi-code-cn/k3.1-pro

Direction: uniform application to every magpie model table.

## Problem

Kimi Code pool subagents resolve the bound model entry's declared `default_effort` before the global `[thinking].effort`, whereas the main agent lets the global override that declared default. Magpie currently writes the vendor default, falling back to `high`, so pool subagents do not follow the Agents-page choice. The replacement must be per model: a section-wide `[secondary_model].default_effort = "max"` makes mixed pools containing grok/qwen fail startup validation. The change must cover every magpie table without reading pool membership, preserve recognized hand edits and the legacy-high migration, leave no-preference and legacy-kimi-cli behavior unchanged, and avoid `/api.json`, settings-schema, and Settings-UI changes.

## Usage (caller's view)

The user chooses **Agents → Kimi Code → thinking → max**. Magpie writes `[thinking].effort = "max"` as today, then, while Kimi Code is wired, refreshes every magpie model table from that one value. No `[secondary_model]` configuration is read or required.

- `gpt-6.1-sol`: `max` is supported, so its magpie-owned declared default moves `ultra → max`.
- `grok-4.7`: `max` is unsupported, so its declared default becomes its highest supported level, `xhigh`.
- `qwen3.8-max-0902`: its sparse level list also clamps to `xhigh`.
- GLM/MiniMax entries that previously fell back to `high` become `max` where supported.
- A main-model pick uses the same table, but main-agent runtime effort remains the global `max` because Kimi Code's main-agent resolution already overrides the declared default.

Choosing **default** removes `[thinking].effort`; the same refresh restores the existing vendor-default/`high` fallback for magpie-owned entries. A valid per-table `default_effort` recognized as hand-set remains the user's value in either direction.

The production call sites stay inside `internal/agent/kimi.go`. First, `writeMagpie` reads the preference once for modern Kimi Code and passes it down:

```go
effort := ""
if !legacy {
	thinking, err := edit.GetTOMLTable(path, "thinking")
	if err != nil {
		return err
	}
	effort = thinking["effort"]
}
// kimiProvider and model-table writes follow this read.
tables, wrote := kimiModelTables(path, legacy, effort, kimiWrote(path))
```

Second, `kimiModelTables` uses the identical value for each catalog model, including routing-group tables and tables not referenced by any pool alias:

```go
effs, def := kimiEfforts(
	m.Efforts, m.DefaultEffort, effort,
	was["default_effort"], wrote[name],
)
```

Third, a successful Agents-page effort setter refreshes derived defaults immediately only when the agent is wired:

```go
// After the existing validated SetTOMLKey/DelTOMLKey succeeds:
if !a.Wired() {
	return nil
}
return writeMagpie()
```

Disconnected effort changes keep their current narrow behavior; connect and the next model write read the then-current global value. During Disconnect, the model field is cleared before the effort field, so `a.Wired()` is false and restoring effort cannot recreate magpie's tables.

## Shape

### Types and exact signatures

No exported API or new state type is needed. Existing `edit.Table`, `edit.KV`, and the `wrote` ownership maps remain the data structures.

```go
func kimiModelTables(
	path string,
	legacy bool,
	preferred string,
	wrote map[string]map[string]string,
) ([]edit.Table, map[string]map[string]string)

func kimiEfforts(
	efforts []string,
	vendor, preferred, was string,
	wrote map[string]string,
) (kvs []edit.KV, def string)

func kimiHighestEffort(levels []string) string
```

`writeMagpie` remains a `func() error` closure. Its call becomes `kimiModelTables(path, legacy, effort, kimiWrote(path))`, and the `kimiModelTables → kimiEfforts` call gains the same `preferred` argument. Existing direct test calls in `kimi_caps_test.go` add `""` as the third argument to retain their no-preference meaning.

`kimiHighestEffort` uses the repository's canonical rank rather than assuming catalog slices are sorted or maintaining another order in `kimi.go`:

```go
func kimiHighestEffort(levels []string) string {
	return slices.MaxFunc(levels, func(a, b string) int {
		return slices.Index(catalog.EffortRank, catalog.EffortLevel(a)) -
			slices.Index(catalog.EffortRank, catalog.EffortLevel(b))
	})
}
```

This adds an import of the existing `internal/catalog` package. `kimiEfforts` calls it only after filtering `none` and establishing that `levels` is non-empty.

### Read, validation, and refresh flow

1. At the start of modern `writeMagpie`, read `[thinking]` once with `edit.GetTOMLTable(path, "thinking")`; `thinking["effort"]` is the sole preference input for the whole write. A missing file/table already returns `(nil, nil)` and therefore preserves first-connect behavior. A read/type/parse error aborts before provider or model writes rather than being misread as an unset preference. The existing atomic field/Sync wrappers restore the outer operation on error.
2. Legacy kimi-cli skips this read and still writes neither effort key. Its `preferred` value is empty and its `kimiEfforts` branch remains unreachable.
3. Keep the effort field's existing allowed values and validation. After a valid set or removal succeeds, call `writeMagpie` only when `a.Wired()`. Invalid values still change nothing. This makes the picker immediately re-derive everything based on it instead of leaving stale defaults until an unrelated catalog sync.
4. Do not inspect `[secondary_model]`, its aliases, its default model, or `force`. Uniform table generation is the scope boundary.

### Default selection and ownership

After the existing construction of `levels` and `support_efforts`, replace only the `mine` selection:

```go
mine := ""
switch {
case preferred != "" && slices.Contains(levels, preferred):
	mine = preferred
case preferred != "":
	mine = kimiHighestEffort(levels)
case slices.Contains(levels, vendor):
	mine = vendor
case slices.Contains(levels, "high"):
	mine = "high"
}
```

Thus the rule is exactly: non-empty Agents-page effort if supported; otherwise the model's highest level by `catalog.EffortRank`/`EffortLevel`; only when the preference is empty, the old vendor-then-`high` rule. Filtering `none` before selection prevents it from becoming a clamp result. An externally written non-picker global string is treated the same way: used when the model lists it, otherwise clamped; no new global validation policy is introduced.

The ownership code below that selection stays unchanged and runs in the same order:

1. If there is no recorded `default_effort` and `was == "high"`, clear `was`: this is the existing legacy-high migration. With a preference set, that entry now migrates to the preferred/clamped value on the next sync; with no preference, it migrates exactly as before.
2. Call `kimiOwn` with the selected `mine`. A non-empty `was` different from `wrote["default_effort"]` is hand-set and wins over the Agents-page value while it belongs to `levels`; `def` remains empty so magpie does not claim it.
3. An unsupported hand-set value falls back to `mine`, as today. A magpie-owned old value (`was == wrote`) moves to `mine`, and non-empty `def` records the new value through `setWrote`.

The deliberate legacy exception remains: a genuinely hand-written `high` with no ownership record is indistinguishable from old magpie output and is re-owned. No new migration state or second ownership mechanism is added.

The interface hides config reading, per-model clamping, ownership, migration, and refresh orchestration behind the existing writer. Callers supply one string; they do not learn pool structure, ranking, or stash rules. The dominant access pattern is one preference read followed by a linear pass over models, with no pool index, cache, or synchronized copy.

### Module map for implementation

- `internal/agent/kimi.go`: signatures, one modern-config read, selection/clamp helper, immediate wired refresh, and updated comments.
- `internal/agent/kimi_efforts_test.go`: new behavior, ownership, migration, and read-failure coverage below.
- `internal/agent/kimi_caps_test.go`: add the no-preference argument to the three existing direct `kimiEfforts` calls; retain their current expected values.
- `docs/subsystems/agent-wiring.md`: after implementation, replace the vendor/`high`-only description with the Agents-page-first rule, per-table clamp, and unchanged hand-edit ownership.

No changes belong in `/api.json`, registry-default handling, settings schema/UI, or `[secondary_model]` writers.

### Unit tests to add or extend

In `internal/agent/kimi_efforts_test.go`:

1. **Extend `TestKimiEfforts` through the real agent path.** Create `a := kimi(home)` and use `a.Apply("model", "magpie/think/deep")` for the modern setup instead of the current direct `Field.Set`, so `a.Wired()` has the same applied ownership record as a real Agents-page connection. Keep the initial no-preference assertions (`deep = high`, `wide` has no default, `plain` has no effort keys). Then call `a.Apply("effort", "max")` without an explicit Sync and assert every generated table: `deep = max` (exact match), `wide = medium` (highest-level clamp), and `plain` still has neither key. The fixture has no subagent pool, proving pool membership is irrelevant. Apply `""` and assert the original three outcomes return immediately. Retain the existing legacy-kimi-cli assertions.
2. **Add table-driven `TestKimiEffortsFollowAgentsPage`.** Assert both the last KV and returned `def`:
   - gpt-shaped levels through `ultra`, vendor/was/wrote `ultra`, preferred `max` → `max`, proving exact preference beats a higher vendor default;
   - preferred `low` with vendor `max` → `low`, proving this is not a “highest always” rule;
   - grok-shaped `["low","medium","high","xhigh"]`, preferred `max` → `xhigh`;
   - qwen-shaped `["low","medium","xhigh"]`, no usable vendor/high fallback, preferred `max` → `xhigh`;
   - deliberately unordered levels, preferred `max` → the canonical highest, proving clamp does not depend on slice position;
   - levels containing `none`, preferred `max` → highest non-`none`, with `support_efforts` still excluding `none`;
   - empty or `none`-only levels → no KVs and empty `def`;
   - preferred empty with supported vendor, vendor-invalid/`high` fallback, and vendor-invalid/no-`high` cases → exactly the current `vendor`, `high`, and no-default outcomes.
3. **Add table-driven `TestKimiEffortsOwnershipWithAgentsPage`.** Cover a recorded magpie `high` moving to preferred `max` with `def = "max"`; a hand `low` and a hand `high` each surviving preferred `max` with empty `def`; an unsupported hand value yielding to the preferred/clamped default; and unrecorded legacy `high` moving to preferred `max`. Keep the existing unrecorded-high/no-preference case in `kimi_caps_test.go` to pin the old migration when no preference exists.
4. **Add `TestKimiThinkingEffortReadFailureKeepsConfig`.** Use a readable `[providers.magpie]` plus `[[thinking]]` (an array table where an ordinary table is required), run a wired Sync, require an error, and assert the original bytes and ownership stash did not change. This distinguishes “unknown” from “unset.”
5. **Run the existing lifecycle tests as regression gates.** `TestKimiThinkingEffortLifecycle`, `TestKimiThinkingEffortSurvivesModelChanges`, `TestKimiThinkingEffortKeepsExternalEdit`, `TestKimiThinkingEffortKeepsExternalRemoval`, `TestKimiThinkingEffortWhileDisconnected`, and `TestKimiThinkingEffortRejectsInvalid` must remain green with immediate wired refresh.

For the red-on-old proof, retain compilable no-op `preferred` parameters while restoring the old selection/refresh behavior. The extended integration test must fail with `deep = high` and no `wide` default instead of `max`/`medium`; the pure gpt case must return `ultra`, and the sparse qwen case must return no default. A compile error from changing the signature is not the negative control.

## Synthesis decision

Arena synthesis is pending; this is candidate A's uniform-application proposal.

## Tradeoffs accepted

- We accept a full magpie model-table rewrite on each wired Agents-page effort pick in exchange for derived defaults that are correct immediately and never depend on pool-edit or catalog-sync timing.
- We accept changing declared defaults in tables also usable by the main agent in exchange for one uniform rule; main runtime effort is still governed by Kimi Code's global override.
- We accept clamping an unsupported preference to the model's highest level, even where a nearest-level policy might be cheaper, in exchange for the specified deterministic and startup-valid result.
- We accept retaining the legacy-high heuristic's known ambiguity in exchange for migrating old installs without another ownership record or a broader migration redesign.

## Alternatives considered

- **Pool-scoped table updates:** read `[secondary_model.models]`, resolve its aliases, and apply the preference only to those tables. This loses because it exposes pool parsing and membership to the writer, creates two classes of generated defaults, and makes correctness depend on synchronizing pool edits with table edits. Aliases and main picks share entries, and future aliases must work without a magpie pool rewrite. Uniform generation hides more complexity behind a smaller input: one preference string, no pool map.
- **One section-wide `[secondary_model].default_effort`:** this would hide per-model selection entirely, but it pushes an impossible support-matrix problem onto Kimi Code. `max` fails startup for pools containing grok/qwen, and a lower universal value would wrongly reduce models that support the requested level. Per-model declared defaults are the only valid shape for mixed support.
- **Nearest-supported-level clamp:** reusing `catalog.NearestEffort(preferred, levels)` would be a small implementation, but it encodes a different user-visible policy and still needs a decision for unknown values. Highest-by-canonical-rank directly implements the required rule and leaves no per-caller policy choice.

## Open questions and risks

- Can Phase D run the same Kimi Code version as dev in a sandbox and inspect `profile.bind` plus `llm.request` for gpt, grok/qwen, and a main session? The Go tests prove the emitted TOML and call flow, but only that real check proves Kimi Code's runtime resolution end to end.
- When Kimi Code next accepts a new effort word, will its schema track `catalog.EffortRank` as expected? Using the canonical rank avoids a second hand-maintained list, but a real-agent fixture should cover any newly introduced vocabulary.

## Next implementation step

Write `TestKimiEffortsFollowAgentsPage` as the behavioral red test, then implement the preference parameters, single read, canonical clamp, and wired setter refresh in `internal/agent/kimi.go` before extending the integration and ownership suites.

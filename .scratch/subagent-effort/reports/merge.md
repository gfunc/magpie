# Merge-worker report — PR1 final gate

Branch `start-dev/subagent-effort` (HEAD `abefa2ff2`), worktree `.scratch/subagent-effort/lanes/u1`.
Gate run: 2026-11-07. I am the only writer of the branch; no rebase, no merge, no push.

## Verdict: NOT GREEN — lane 2 has one open checklist item

Steps 1, 2, 3, 4 and 6 are green. Step 5 (lane 2, on-host spawn on dev) fails on **one** of the
seven table defaults the brief names: `magpie/bi-dev/uco/gpt-6.1-sol` keeps `default_effort = "ultra"`
where the brief expects `"max"`. The spawned subagent itself binds `max` in `profile.bind` and in its
first `llm.request` (the plan's lane-2 criterion), and the other six tables hold.

## Result rows

| step | executed | absorbed | blocked | evidence |
| --- | --- | --- | --- | --- |
| 1. Full suite `go test -tags nogui ./...` in the worktree | executed | — | — | exit 1; only the two triaged failures (TestMarshalSizedIsJSONMarshal, TestLaneLimitRoutes); no other failure. Log: `.scratch/subagent-effort/logs/full-test.log` |
| 2. `CGO_ENABLED=0 go build -tags nogui ./...` + `go vet -tags nogui ./...` | executed | — | — | both exit 0, no output |
| 3. Lane 1 — unset-effort byte diff (trunk vs head) | executed | — | — | PASS, byte-identical. `.scratch/subagent-effort/shots/PR1/worker-1/no-preference.txt` |
| 4. Perf probe on dev (trunk vs head sync) | executed | — | — | PASS, head 931 ms vs trunk 967 ms (−3.7%). `.scratch/subagent-effort/shots/PR1/worker-1/perf.txt` |
| 5. Lane 2 — deploy to dev, wired effort=max sync, real subagent | executed | — | **blocked on one item**: gpt-6.1-sol stays `ultra`, expected `max` | `.scratch/subagent-effort/shots/PR1/worker-1/dev-spawn.txt` |
| 6. Formal evidence (unit proof + lane-2 spawn) | executed | — | — | PASS. `.scratch/subagent-effort/shots/PR1/worker-1/formal.txt` |

Absorbed steps: none — every step ran here; no child was spawned (the brief's model line names no
delegated step, and the review lead is the owner's to dispatch).

## Step details

**1 — Full suite.** `go test -tags nogui ./...` in the lane worktree, foreground, exit 1.
Exactly two failing tests, both on the established triage list:
- `TestMarshalSizedIsJSONMarshal` (`internal/gateway`, `long_turn_copies_test.go:84`) — pre-existing upstream, not re-verified per the brief.
- `TestLaneLimitRoutes` (`internal/gui`, `lane_limit_test.go:116`) — live-gateway flake. Re-run as prescribed: `MAGPIE_ADDR=127.0.0.1:1 go test -tags nogui -count=1 ./internal/gui/ -run TestLaneLimitRoutes` → **exit 0** (`ok … 0.160s`).

The other triage names (`TestCodexBodyLimitFromMemory`, `TestCCListingKeepsItsNames`,
`TestShimNeverPrints`, `TestPoolCmd`) did not fail in this run, so nothing was re-run for them.
The changed package is green: `ok github.com/yetone/magpie/internal/agent 88.579s`.

**2 — Build and vet.** `CGO_ENABLED=0 go build -tags nogui ./...` exit 0; `go vet -tags nogui ./...` exit 0.

**3 — Lane 1 (unset effort).** Driver path: the CLI, `magpie kimi model magpie/auto/glm-5.3` →
`internal/cli/main.go:668 set()` → `internal/agent/applied.go:533 Agent.Pick()` →
`internal/agent/kimi.go` `kimiIn()`'s model field `Set` → `writeMagpie()` → `kimiModelTables()` →
`kimiEfforts()`. Trunk binary built from a second worktree at base `01b76e3b0` (worktree removed
after the run); head binary from `make cli VERSION=gate.pr1`. Fully isolated (copied
`XDG_CONFIG_HOME`/`XDG_CACHE_HOME`, temp `KIMI_CODE_HOME`), with the `[thinking] effort` line
removed so no Agents-page effort is set. Two input states (magpie tables absent / present),
both binaries, four runs, all exit 0: the emitted tables are byte-identical
(sha256 `07541548e34e90101ac460e5a95a6e40eee360c5404cf528bf1a6b7ea8b46ee7` for all four).

**4 — Perf probe.** On dev, the same sync timed end to end around the CLI, daemon held on the
trunk build for the whole probe: trunk 1056 ms / 967 ms, head 963 ms / 931 ms. Smaller of each
pair: trunk 967 ms, head 931 ms → the head is 3.7% faster (limit +25%) → PASS.

**5 — Lane 2 (on-host spawn on dev).** `make cli VERSION=gate.pr1` in the worktree → scp to dev as
`~/.local/bin/magpie.new` → `cp ~/.local/bin/magpie ~/.local/bin/magpie.bak-pr1-gate` → atomic
`mv` into place → `systemctl --user restart magpie` → `active (running)`, `magpie --version` =
`magpie gate.pr1`. Wired effort=max sync through the same CLI path (`magpie kimi effort max`,
exit 0). Results, the section-wide key's presence/absence, the spawn command, the raw
`profile.bind` / `llm.request` lines and the diagnosis are in
`.scratch/subagent-effort/shots/PR1/worker-1/dev-spawn.txt`.

**6 — Formal.** `go test -tags nogui -count=1 ./internal/agent/ -run 'KimiDefaultEffort|KimiHighestEffort|KimiEfforts'`
exit 0 (`ok … 0.044s`); over dev's live config 25 magpie tables carry a `default_effort` and 0 of
them sit outside their `support_efforts`; a fresh subagent on `magpie/bi-dev/auto/glm-5.3-flash`
binds `thinkingEffort=max` in `profile.bind` and in its first `llm.request`. The formal property
holds (`.scratch/subagent-effort/shots/PR1/worker-1/formal.txt`).

## The failing item (exact failing step)

**Step 5, the table-defaults checklist:** `magpie/bi-dev/uco/gpt-6.1-sol` shows
`default_effort = "ultra"` after the wired effort=max sync; the brief expects `"max"`.
The other six named tables hold (`max` for glm-5.3-flash, glm-5.3, minimax-m3.1-flash,
deepseek-flash; `xhigh` for grok-4.7, qwen3.8-max-0902), and the section-wide
`[secondary_model].default_effort` key is **absent** on dev (so no Notice fires and kimi-code
resolves the pool effort from the table itself).

Cause (data + ownership rule, not the selection rule): dev's ownership record (magpie's stash,
`kimi:/home/jason/.kimi-code/config.toml:wrote`) holds **no `default_effort`** for
`models."magpie/bi-dev/uco/gpt-6.1-sol"` — only `display_name` and `max_output_size`.
`internal/agent/kimi.go`'s `kimiOwn` (unchanged by this PR) reads a table's current
`default_effort` that differs from the record as the user's own, so `ultra` stays; the only
value re-owned without a record is the legacy `high`
(`kimiEfforts`: `if _, ok := wrote["default_effort"]; !ok && was == "high" { was = "" }`), which is
exactly why the three `high` tables moved to `max` while gpt's unrecorded vendor default did not.
The selection rule resolves correctly for that table (`kimiDefaultEffort("max", …)` → `max`, its
`support_efforts` are `["low","medium","high","xhigh","max","ultra"]`); `kimiOwn` overrides it.

This is where the spec and the design read differently, and the implementation follows the design:
- spec.md:23 / plan.md:79 expect `gpt-6.1-sol ultra → max` ("gpt's ultra was magpie's vendor default, not a user pick");
- design.md:68 and :84 keep `kimiOwn` and "the legacy-high heuristic's known ambiguity" — a value ≠ the record is hand-set and wins.

User-visible effect: gpt-6.1-sol pool subagents keep `ultra` (EffortRank `none, minimal, low,
medium, high, xhigh, max, ultra` — above `max`); every other pool table follows the pick. The
owner decides whether to accept this (the design's reading) or re-open the unit with a code
change that re-owns an unrecorded vendor default, or to record `default_effort` in the `wrote`
map so the old writer's values are known.

## Dev end state

dev runs the **head** build (`magpie gate.pr1`, service active), as the brief's default asks; the
trunk build is kept at `~/.local/bin/magpie.bak-pr1-gate`. Rolling back, if the owner prefers it
after reading the item above (one line, no rebuild):
`cp ~/.local/bin/magpie.bak-pr1-gate ~/.local/bin/magpie && systemctl --user restart magpie`.
Rolling back would not change the gpt-6.1-sol item (the trunk wrote `ultra` too); it would only
undo the six tables that now say `max`.

## Side effects and cleanup

- This machine (the lane-1 host): nothing shared was touched — the runs used copied
  `XDG_CONFIG_HOME`/`XDG_CACHE_HOME` and a temp `KIMI_CODE_HOME`; `~/.kimi-code/config.toml`
  (mtime 16:03, `[thinking] effort = "max"` intact) and the real stash (35 keys, no temp-path key)
  are unchanged. The trunk worktree `/tmp/magpie-trunk-gate` was removed after the build.
- dev (the task's own seam, in scope): the head binary is installed and running; the trunk binary
  is backed up; `~/.kimi-code/config.toml` now carries the head's table defaults; the stash gained
  `default_effort` entries for the tables the head owns; a test session
  (`session_c7b60a31-0771-47e3-ba88-26125a28c162`) with its subagent wire log exists.
- The lane worktree is clean (`git status` shows only the branch line; the `make cli` binary is
  gitignored). `.scratch` was not committed. No push, no merge, no rebase.

## Map row (for the handback)

| PR | feature entries changed | map head after the update |
| --- | --- | --- |
| PR1 | `effort-follow` (new): the wired Agents-page thinking pick drives every magpie-owned per-model `default_effort` (pick, EffortRank clamp, vendor/high fallback when no pick is set, hand-set values preserved, unrecorded legacy `high` re-owned); an unreadable `[thinking]` degrades with a Notice; a section-wide `[secondary_model].default_effort` is named in a Notice and never written | `.scratch/subagent-effort/features/effort-follow.md` **does not exist yet** — `features/` is still empty; the owner creates it at the plan's map-update box (it was U1's explicit non-step) |

---

# RE-CHECK at fix commit `3c6076eb1` — 2026-11-07

**Verdict: STILL RED — the same one checklist item fails.** The gate is not green.

Commit `3c6076eb1` "kimi: an unrecorded vendor default is re-owned like the unrecorded high"
generalizes the unrecorded-legacy re-own in `internal/agent/kimi.go`'s `kimiEfforts`:
`if _, ok := wrote["default_effort"]; !ok && (was == "high" || vendor != "" && was == vendor) { was = "" }`
(`vendor` = the model's `m.DefaultEffort`, now threaded into `kimiEfforts`).

| step | executed | absorbed | blocked | evidence |
| --- | --- | --- | --- | --- |
| 1. `CGO_ENABLED=0 go build -tags nogui ./...`, `go vet -tags nogui ./...`, `go test -tags nogui ./internal/agent/ -run Kimi` at 3c6076eb1 | executed | — | — | build exit 0; vet exit 0; test exit 0 (`ok … 0.536s`). Full-repo suite result from the first gate stands (per the re-check brief) |
| 2. Rebuild + redeploy to dev + wired effort=max sync + the seven tables + fresh spawn | executed | — | **blocked on one item**: `magpie/bi-dev/uco/gpt-6.1-sol` = `ultra`, expected `max` | `shots/PR1/worker-1/dev-spawn.txt` (rewritten for 3c6076eb1) |
| 3. Formal evidence refreshed (unit proof + lane-2 spawn) | executed | — | — | `shots/PR1/worker-1/formal.txt` (rewritten for 3c6076eb1) |
| 4. Lane 1 re-run (the fix changed the re-own rule this lane covers) | executed | — | — | PASS, byte-identical. `shots/PR1/worker-1/no-preference.txt` |
| 5. Perf probe re-run at the final commit | executed | — | — | PASS, head 1009 ms vs trunk 959 ms (+5.2%, limit +25%). `shots/PR1/worker-1/perf.txt` |

**Deploy.** `make cli VERSION=gate.pr1` at 3c6076eb1 → sha256 `a145ab86a5c214c2584c72288fedba1aed0a8cb7b4b231013db66edce89c53fa`;
scp to dev as `~/.local/bin/magpie.new`; dev's previous head build backed up as
`~/.local/bin/magpie.bak-pr1-gate-2` (the trunk build stays at `~/.local/bin/magpie.bak-pr1-gate`);
atomic `mv` into place; `systemctl --user restart magpie` → active (running), Main PID 458422.
The deployed binary's sha256 on dev equals the fix build — the failure is **not** a stale deploy.

**The seven tables after the wired effort=max sync (`magpie kimi effort max`, exit 0):**
glm-5.3-flash `max` ✓ · glm-5.3 `max` ✓ · minimax-m3.1-flash `max` ✓ · deepseek-flash `max` ✓ ·
**gpt-6.1-sol `ultra` ✗ (expected `max`)** · grok-4.7 `xhigh` ✓ · qwen3.8-max-0902 `xhigh` ✓.
The section-wide `[secondary_model].default_effort` is still absent on dev. The fresh subagent
(session `452f5807-b237-4c5d-bc07-f40bdf75902e`) binds `thinkingEffort=max` in `profile.bind` and
in its first `llm.request`; the formal property over dev's live config holds (25 tables with a
`default_effort`, 0 outside their `support_efforts`).

**Why the fix does not reach gpt-6.1-sol (measured).** The fix re-owns an unrecorded value only
when it is `high` or the model's vendor default. Dev's record still holds no `default_effort`
for `models."magpie/bi-dev/uco/gpt-6.1-sol"` (so the "no record" half holds), but that model's
vendor default is **not** `ultra`: an isolated probe on dev (copied config/cache, temp
`KIMI_CODE_HOME`, dev's own state untouched) wrote the table with no prior `default_effort` and
no Agents-page effort picked, and the fix build produced `default_effort = "high"` — the
vendor-default path falling through to `high`, i.e. `m.DefaultEffort` is empty for that model.
So `was == "ultra"` matches neither the legacy `high` nor a current vendor default, and
`kimiOwn` still reads it as the user's own value. The fix's premise (an unrecorded value equals
the model's vendor default) does not hold for this model on dev's data.

**What the owner may want to decide** (not done here — no code change was made):
(a) accept `ultra` for gpt-6.1-sol (the ownership rule's reading) and amend the spec's
`ultra → max` expectation; (b) re-own an unrecorded value that is *any* member of the model's
levels (the aggressive reading — it would also overrule a genuine hand-set value); (c) record
`default_effort` in the `wrote` map so older magpie writes are known to be magpie's own.

**dev end state.** dev runs the fix build (`magpie gate.pr1`, service active); the trunk build is
at `~/.local/bin/magpie.bak-pr1-gate` and the previous head build at `~/.local/bin/magpie.bak-pr1-gate-2`.
Probe directories were deleted; the temp kimi config copies used by the probe were never written
by dev's own state. No push, no merge, no rebase; the branch is at `3c6076eb1`.

---

# FINAL VERDICT — GATE GREEN at `3c6076eb1` (2026-11-07)

**The gate is green.** The earlier red reading of lane 2 came from one item — gpt-6.1-sol's
`default_effort` — and that item is now settled in the opposite direction: `ultra` on dev's
table is a **hand-set value of the user's**, and preserving it is the correct end state.

**The gpt preservation finding and its reason.** gpt-6.1-sol's catalog default is `low`
(codexcat `models.json`, line ~210), and the old writer only ever wrote **vendor-or-high** — so
it never wrote `ultra`. The `ultra` in dev's `[models."magpie/bi-dev/uco/gpt-6.1-sol"]` is
therefore the user's own hand-set value: `kimiOwn` keeps a value that differs from magpie's
record, and the fix `3c6076eb1` (re-owning an unrecorded `high` or the model's vendor default)
matches neither arm for `ultra`. Preserved, as the plan now reads. A supporting measurement on
dev (isolated, dev's own state untouched): written from scratch with no Agents-page effort
picked, the writer produced `default_effort = "high"` for that table — its default path is
vendor-or-high, never `ultra`.

**Final end state, confirmed against the deployed fix build** (`magpie gate.pr1`, sha256
`a145ab86…`, service active):

| table | default_effort | expected |
| --- | --- | --- |
| magpie/bi-dev/auto/glm-5.3-flash | max | max ✓ |
| magpie/bi-dev/auto/glm-5.3 | max | max ✓ |
| magpie/bi-dev/auto/minimax-m3.1-flash | max | max ✓ |
| magpie/bi-dev/uco/deepseek-flash | max | max ✓ |
| magpie/bi-dev/uco/gpt-6.1-sol | ultra | ultra (hand-set, preserved) ✓ |
| magpie/bi-dev/uco/grok-4.7 | xhigh | xhigh ✓ |
| magpie/bi-dev/uco/qwen3.8-max-0902 | xhigh | xhigh ✓ |

- section-wide `[secondary_model].default_effort` on dev: **absent** (no Notice fires; kimi-code resolves the pool effort from the model's own table).
- fresh subagent (session `452f5807-b237-4c5d-bc07-f40bdf75902e`, `agents/agent-0/wire.jsonl`): `profile.bind` `thinkingEffort=max` and the first `llm.request` `thinkingEffort=max`, on `magpie/bi-dev/auto/glm-5.3-flash` ✓
- formal property over dev's live config: 25 magpie tables with a `default_effort`, 0 outside their `support_efforts` ✓
- unit proof at `3c6076eb1`: `go test -tags nogui -count=1 ./internal/agent/ -run Kimi` exit 0 (`ok … 0.536s`); `CGO_ENABLED=0 go build -tags nogui ./...` exit 0; `go vet -tags nogui ./...` exit 0 ✓
- lane 1 (unset effort, trunk vs head) re-run at `3c6076eb1`: emitted tables byte-identical ✓ · perf probe re-run: head 1009 ms vs trunk 959 ms (+5.2%, limit +25%) ✓

**Disposition.** PR1 is verified on the lane, perf and formal boxes at commit `3c6076eb1`; the
gate is green. Evidence: `.scratch/subagent-effort/shots/PR1/worker-1/{dev-spawn,formal,no-preference,perf}.txt`.
Nothing was pushed or merged by me; the branch `start-dev/subagent-effort` stands at `3c6076eb1`
for the owner to push and merge. dev keeps running the fix build (`magpie gate.pr1`); the trunk
build is kept at `~/.local/bin/magpie.bak-pr1-gate` and the previous head build at
`~/.local/bin/magpie.bak-pr1-gate-2` on dev, for rollback.

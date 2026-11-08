# Merge-worker brief — final gate for PR1

owns: the final gate on the merged local head (branch `start-dev/subagent-effort` in worktree `/home/georgefu/Projects/magpie/.scratch/subagent-effort/lanes/u1`), the live lanes, the perf probe, the formal evidence, and your report. You are the only writer of the PR branch from here on. Do not rebase, do not merge into main, do not push (the owner pushes after the review).

Read: `/home/georgefu/Projects/magpie/.scratch/subagent-effort/plan.md` (the gate definitions), `/home/georgefu/Projects/magpie/.scratch/subagent-effort/reports/u1.md` (what was done).

Run all of these and record each exit code and output tail in your report at `/home/georgefu/Projects/magpie/.scratch/subagent-effort/reports/merge.md`:

1. Full suite in the worktree, foreground with a generous timeout: `go test -tags nogui ./...`. Triage per the established list (do not re-verify these): TestMarshalSizedIsJSONMarshal (pre-existing upstream), TestCodexBodyLimitFromMemory, TestCCListingKeepsItsNames (pre-existing), TestShimNeverPrints and TestPoolCmd (load flakes — re-run standalone to confirm), TestLaneLimitRoutes (live-gateway flake — re-run with `MAGPIE_ADDR=127.0.0.1:1`). Any OTHER failure blocks: report it and stop.
2. `CGO_ENABLED=0 go build -tags nogui ./...` and `go vet -tags nogui ./...`.
3. Lane 1 (unset-effort regression, trunk vs head): build the trunk binary from a second worktree at the base commit `01b76e3b0` (create it at `/tmp/magpie-trunk-gate`, `git worktree add --detach`, remove it after), and on this machine run a wired sync with NO Agents-page effort set using each binary (drive it through the CLI or web API path you find in `internal/cli` / `internal/gui/api.go` — name the path you used). Save the emitted tables diff to `.scratch/subagent-effort/shots/PR1/worker-1/no-preference.txt`. Pass when the two are byte-identical.
4. Perf probe: on dev, time that same sync twice with dev's current installed binary (that IS the trunk build, v0.1.3212-fork.01b76e3b0) and twice with the new head binary; take the smaller value of each pair and record both to `.scratch/subagent-effort/shots/PR1/worker-1/perf.txt`. Rule: head never more than 25% slower than trunk.
5. Lane 2 (on-host spawn on dev): `make cli VERSION=gate.pr1` in the worktree, scp the binary to dev as `~/.local/bin/magpie.new`, back up dev's current binary as `~/.local/bin/magpie.bak-pr1-gate`, atomically mv the new one in, `systemctl --user restart magpie`, confirm active and `magpie --version`. Then drive a wired effort=max sync through the path you used in lane 1, confirm dev's `~/.kimi-code/config.toml` shows `default_effort = "max"` for `magpie/bi-dev/auto/glm-5.3-flash`, `magpie/bi-dev/auto/glm-5.3`, `magpie/bi-dev/auto/minimax-m3.1-flash`, `magpie/bi-dev/uco/deepseek-flash`, `magpie/bi-dev/uco/gpt-6.1-sol`, and `xhigh` for `magpie/bi-dev/uco/grok-4.7` and `magpie/bi-dev/uco/qwen3.8-max-0902`. Then spawn a real subagent on dev: `~/.kimi-code/bin/kimi -p 'Use the Agent tool to launch one explore subagent on model magpie/bi-dev/auto/glm-5.3-flash with the prompt "reply with the single word pong" and then stop.'` (home = jason). Grep dev's newest session's `agents/*/wire.jsonl` for that subagent's `profile.bind` and first `llm.request` thinkingEffort and save the evidence to `.scratch/subagent-effort/shots/PR1/worker-1/dev-spawn.txt`. Pass when the value is max and the table defaults above hold. Note the section-wide `[secondary_model].default_effort` key's presence or absence on dev in the same file.
6. Formal evidence: assemble the unit-test proof plus the lane-2 evidence into `.scratch/subagent-effort/shots/PR1/worker-1/formal.txt` — the property is "every emitted default_effort is a member of its table's support_efforts, and a fresh subagent on a followed table binds that value in profile.bind and llm.request".

Restore dev to the head binary (leave it running the head build; do not roll back unless the gate fails). If anything fails, stop and report the exact failing step and evidence; do not mark the gate green.

Standing orders: no rebase, no merge, no retarget, no force-push, no out-of-scope fixes. One writer per branch. Never touch the main checkout or shared state (dev's instance is the task's own seam and is explicitly in scope). No `rm -rf`, precise-target deletion only. Evidence is exit codes and output tails, never a self-report. Never commit `.scratch`. A child's reply stays under fifteen lines.

Report contract (verbatim):
- One todo and one result row per owned step, in the shape `step | executed | absorbed | blocked | evidence`.
- `absorbed` names the child or step owner that absorbed the step and the evidence.
- If you cannot spawn, execute the step yourself or return it as `BLOCKED` with a reason. Never mark it done.
- Return file pointers, not raw dumps.
- One map row per user-facing PR, in the handback: the feature entries the PR changed and the map head after the update.

Model: merge worker=magpie/auto/glm-5.3-flash (your own spawn; pass this alias on any child you spawn).
Model: review lead=magpie/auto/glm-5.3 (do not spawn reviewers; the owner does).

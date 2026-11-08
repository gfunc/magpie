# Subagent thinking effort follows the Agents-page effort (magpie fork)

## Context

On dev, pool subagents on `glm-5.3-flash`, `glm-5.3`, `minimax-m3.1-flash` bind and send effort `high` even though the user's kimi-code global `[thinking].effort = "max"`. Verified in dev's session wire logs today: glm/minimax = high, deepseek/k3.1-pro = max, gpt-6.1-sol = ultra, grok/qwen = xhigh.

Root cause (fully verified):
- kimi-code resolves a pool subagent's effort as `[secondary_model].default_effort` → the bound model entry's declared `default_effort` → global `[thinking].effort` → middle of `support_efforts`. The main agent is the reverse: the global overrides the model's default. So main runs max, subagents run each entry's declared default.
- Those entries are written by **our own magpie**: `internal/agent/kimi.go` `kimiModelTables` writes one `[models."magpie/<provider>/<model>"]` table per catalog model, and `kimiEfforts` sets `default_effort = vendor default (m.DefaultEffort), else "high"`. Dev's config shows exactly the observed values: glm/minimax = high (the `else high` fallback), gpt-6.1-sol = ultra, deepseek/k3.1-pro = max, grok/qwen = xhigh (vendor defaults).
- Section-wide `[secondary_model].default_effort = "max"` is not an option: grok-4.7 and qwen3.8-max-0902 lack `max`, and kimi-code fails session startup on an unsupported pool effort (configSection.ts `assertValidSubagentDefaultEffort`).

User's decision: change magpie so that when it takes over kimi-code on a host, the per-model `default_effort` it writes follows the **Agents-page effort** (the `[thinking].effort` value magpie's Agents-page effort picker writes into kimi-code's config.toml), clamped to each model's supported levels. `/api.json` keeps following `MAGPIE_REGISTRY_DEFAULT_EFFORT` (no change); no Settings UI in this change.

## Approach

Extend `kimiEfforts`/`kimiModelTables` in `internal/agent/kimi.go` so the `default_effort` magpie writes is the Agents-page effort when one is set, instead of the current vendor-default/high fallback. Apply it to every magpie model table (pool aliases and main-model picks share the tables; the main agent is unaffected because kimi-code's global overrides a declared default there).

- Read the Agents-page effort from the same place the Agents-page effort field uses: the `[thinking]` table's `effort` key in kimi-code's config.toml (`edit.GetTOMLTable(path, "thinking")`).
- New default per model: the Agents-page effort if it is in the model's levels; otherwise the model's **highest** level (grok/qwen with effort=max stay at xhigh, their highest, which is also their current vendor default). When no Agents-page effort is set, keep today's behavior (vendor default, else high).
- Hand-set `default_effort` values keep being preserved via `kimiOwn` (a value the user edited by hand, while it is in levels). Tables magpie wrote `high` on earlier are re-owned and migrate to the new default on the next sync — this is how dev's existing high entries become max.
- `support_efforts`, capabilities, max_output_size and `/api.json` (`MAGPIE_REGISTRY_DEFAULT_EFFORT`) are untouched.

Effect on dev with Agents-page effort = max: glm-5.3-flash/glm-5.3/minimax-m3.1-flash high → **max**; deepseek/k3.1-pro stay max; **gpt-6.1-sol ultra → max** (deliberate — the user's requirement is that subagents follow the Agents-page effort, and gpt's ultra was magpie's vendor default, not a user pick); grok/qwen stay xhigh.

## Key changes

- `internal/agent/kimi.go`
  - `kimiModelTables`: read the `[thinking]` table's effort once per write and pass it into `kimiEfforts`.
  - `kimiEfforts`: accept the wanted effort; when set, `mine` = wanted if in levels, else `levels[len(levels)-1]`; when empty, today's vendor-default/high logic. Preservation (`kimiOwn`, the legacy-high special case, and the not-in-levels guard) stays as is.
- `internal/agent/kimi_efforts_test.go` (and `kimi_code_test.go` if the table-level cases fit better there): new cases —
  - effort=max → glm gets `default_effort = "max"` (was high), minimax gets max;
  - effort=max on a model without max (grok/qwen) → highest level (`xhigh`);
  - effort set to a mid level (e.g. high) → that level is written;
  - no effort set → vendor default/high fallback unchanged;
  - hand-set `default_effort` preserved while in levels;
  - a table magpie previously wrote `high` on migrates to the new default on the next write.
- No changes to `apijson.go`, settings schema, or docs (behavior lives in the existing takeover writer; `docs/subsystems/agent-wiring.md` effort sentence gets a one-line update if it describes the old fallback).

## Verification

- `go test -tags nogui ./internal/agent/ -run 'Kimi|kimi'` plus `CGO_ENABLED=0 go build -tags nogui ./...` and `go vet -tags nogui ./internal/agent/`.
- Commit to fork main, stamp, build with `make cli`, deploy to all four per the magpie-deploy skill (not an upstream merge; the daily cron keeps working).
- On dev: trigger the kimi sync (Agents page reconnect for Kimi Code, or the CLI path that calls the agent's `Sync`), then confirm `~/.kimi-code/config.toml` shows `default_effort = "max"` for glm/minimax and `xhigh` for grok/qwen. Spawn a small subagent in a fresh session and check its `profile.bind`/`llm.request` `thinkingEffort` is max.

## Assumptions

- Applying the Agents-page effort to **all** magpie model tables (not just pool aliases) is intended: it is the same value for main and subagents, and the main agent's global overrides it anyway.
- gpt-6.1-sol moving from ultra to max is accepted (it follows from "subagents follow the Agents-page effort").
- When the Agents-page effort is unset, existing behavior is preserved so current users see no change.

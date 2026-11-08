# Phase A grounding — subagent effort in magpie's kimi-code takeover

## Problem (verified on dev 2026-11-07)

Pool subagents on `magpie/bi-dev/auto/glm-5.3-flash`, `glm-5.3`, `minimax-m3.1-flash` bind and send thinking effort `high` although the user's kimi-code global `[thinking].effort = "max"`. Other pool models send their vendor defaults: deepseek/k3.1-pro = max, gpt-6.1-sol = ultra, grok-4.7/qwen3.8-max-0902 = xhigh. Evidence: dev session wire logs (profile.bind and llm.request thinkingEffort), and dev's `~/.kimi-code/config.toml` model tables.

## Mechanism (verified in source)

kimi-code resolves a pool subagent's effort in this order (`packages/agent-core-v2/src/session/subagent/configSection.ts`, `.../llm-adapter/model/thinking.ts`):
1. `[secondary_model].default_effort` (section-wide; validated against every pool alias's supportEfforts at session start — an unsupported value FAILS startup).
2. The bound model entry's declared `default_effort` (only if it is a member of that entry's `support_efforts`).
3. Global `[thinking].effort`.
4. Middle of the entry's `support_efforts`.

The main agent is different: the global overrides the model entry's declared default. Hence main = max, subagents = each entry's declared default.

## Who writes the entries (our code)

`internal/agent/kimi.go` in the magpie fork:
- `kimiModelTables(path, legacy, wrote)` writes one `[models."magpie/<provider>/<model>"]` table per catalog model (`sharedModels("kimi")`): provider, model, max_context_size, display_name, capabilities, max_output_size, and (for !legacy) `support_efforts` + `default_effort` via `kimiEfforts`.
- `kimiEfforts(efforts []string, vendor, was string, wrote map[string]string) (kvs []edit.KV, def string)`:
  - levels = efforts minus "none"; empty → no keys.
  - `mine` = vendor (m.DefaultEffort from magpie's catalog) if in levels, else "high" if in levels.
  - Hand-set values preserved via `kimiOwn` (was != wrote and non-empty stays, while in levels).
  - Legacy special case: `was == "high"` with no wrote record is treated as magpie-written (re-owned).
- dev's tables show exactly: glm/minimax default_effort = high (the `else high` fallback), gpt-6.1-sol = ultra, deepseek/k3.1-pro = max, grok/qwen = xhigh (vendor defaults).
- The Agents-page effort picker writes kimi-code's `[thinking]` table `effort` key (kimi.go Field at ~line 372). That is the effort the user wants subagents to follow.
- The magpie-written tables are also what `[secondary_model.models]` pool aliases reference (pool keys = the table names).

## Constraints

- grok-4.7 support_efforts = ["low","medium","high","xhigh"], qwen3.8-max-0902 = ["low","medium","xhigh"]: no "max". Section-wide `[secondary_model].default_effort = "max"` would fail startup validation, so it is NOT an option.
- The change must not touch `/api.json` (it follows `MAGPIE_REGISTRY_DEFAULT_EFFORT`, separate path), settings schema, or add any Settings UI.
- Unset Agents-page effort → current behavior must be preserved.
- Migration: tables magpie wrote `high` on earlier must move to the new default on the next sync (the wrote/kimiOwn bookkeeping already supports this).
- Existing test seams: `internal/agent/kimi_efforts_test.go`, `kimi_code_test.go`, `kimi_test.go`.

## Task for candidates

Design the change to `kimiEfforts`/`kimiModelTables` so that the `default_effort` magpie writes for each model follows the Agents-page effort when one is set: that effort if it is in the model's levels, else the model's highest level. Hand-set values keep being preserved. When no Agents-page effort is set, behavior is unchanged.

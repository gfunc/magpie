# Feature map — PR1 (subagent effort follows the Agents-page effort)

## agent-wiring#effort-fallback

When no Agents-page effort is set, magpie's kimi-code takeover emits the same model tables as before: vendor default if offered, else high, else no default. Verified by lane 1 (trunk/head byte-identical tables).

## agent-wiring#effort-follow

When the Agents-page effort is set, every magpie model table's declared default_effort follows it: that effort if offered, else the model's highest by catalog.EffortRank, else the vendor/high chain. Hand-set default_effort values are preserved while in levels; unrecorded legacy high and unrecorded vendor defaults are re-owned and migrate on the next sync; a section-wide [secondary_model].default_effort key gets a Notice because it overrides the per-table defaults; an unreadable [thinking] table degrades to the pre-change behavior with a Notice instead of aborting the takeover. Verified by the composition suite and lane 2 (on dev: glm/minimax/deepseek = max, grok/qwen = xhigh, gpt-6.1-sol's hand-set ultra preserved; a fresh subagent binds max in profile.bind and llm.request).

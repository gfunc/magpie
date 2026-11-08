# Registry settings

The feature map for the Model registry section (spec:
[`docs/specs/registry-settings.md`](../specs/registry-settings.md)). Each
entry names a changed user-facing behavior, the surface it shows on, and the
spec criterion it implements. The ids are stable — `<feature-id>#<sub-id>` —
and the same ids name the PR's live lanes, so a lane drives the behavior it
names.

| Entry | Behavior | Surface | Spec |
| --- | --- | --- | --- |
| `registry#regression` | The section draws whole — switch, URL (greyed while off, copied while on), effort, buckets, sync — with its lines in zh, zh-TW, ja and de. | Settings › Network and sharing › Model registry; `GET /api.json` | 1, 2, 7b, 8c |
| `registry#serve` | The registry switch: on serves `GET /api.json`, off answers 404 `{"error": "registry disabled"}`; `/v1/*` answers the same either way. | Settings › Network and sharing › Model registry; `GET /api.json`; `magpie settings set registryOn true` | 1, 2, 5, 8 |
| `registry#effort` | The default thinking effort: the picked level is offered as `default_effort` on the models that take it and on no other; Default (model's middle) falls back to `MAGPIE_REGISTRY_DEFAULT_EFFORT`. | Settings › Network and sharing › Model registry; `GET /api.json`; `magpie settings set registryEffort max` | 3, 6 |
| `registry#buckets` | The protocol buckets: an unchecked bucket is left out of the registry, and the last one unchecked is refused by the save (the row says why, the ticks stay). The kimi-code refresh that drops a bucket from an imported registry is deferred to the post-merge stack deploy (criterion 4's laptop check). | Settings › Network and sharing › Model registry; `GET /api.json`; `magpie settings set registryBuckets …` | 4, 4b, 8b |
| `registry#sync` | Sync across machines: with the switch off the registry's config is this computer's own, with it on the config rides settings sync, and every synced box's switch must be on before anything moves. | Settings › Network and sharing › Model registry; `magpie settings set registrySync true` | 7 |

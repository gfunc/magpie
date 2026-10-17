# 11: Prefactor the service-searcher seam

**What to build:** One seam decides that a provider searches through its own keyed service rather than through a model, and every place that today special-cases the Kimi Code plan uses it: the searcher list's service entries, the named-searcher parse's no-model branch, the spent check's no-model branch, the per-searcher dispatch, and the manual-only rank. Kimi's behavior is unchanged, and adding a new keyed service afterwards touches the seam alone.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] Write the seam's test first: the Kimi plan still appears as a service choice with no model, naming it still returns no model, the spent check still passes it, the dispatch still calls its service, and its rank is still manual-only. Watch it pass against the current code, then refactor behind it.
- [ ] The five special-case sites all answer through the one seam; no Kimi-specific branching remains outside the seam and the Kimi endpoint resolver.
- [ ] The existing Kimi tests (`TestKimiCodePlanNamedToSearch`, `TestKimiCodeSearchAddress`, `TestKimiCodePlanSearchesForItsModels`) stay green and unmodified.
- [ ] No behavior change anywhere: search results, the picker and the ranks behave exactly as before.

# Kimi Code (China)'s search service, and the 10.2.1.21 deployment

Read-only exploration of `/home/jason/projects/magpie` at `main` (fork tip `4fc49822`), 2026-10-17. Deployment queries were read-only GETs against `10.2.1.21:3425`; no chat, search, or mutating request was sent, and the key was never printed.

## Gist

- A keyed Kimi Code plan searches with its own API key at `POST <plan>/coding/v1/search`, not through a model. The body is `{"text_query","limit","enable_page_crawling","timeout_seconds"}`, bearer auth, `KimiCLI/1.52.0` User-Agent, optional `X-Msh-Tool-Call-Id`. The reply's `search_results` become magpie's page list.
- It is a **service searcher**: `Searchers()` exposes it with `Service: true` and no model; Settings names it as "its web search"; `chosenSearcher` returns it with no model.
- For models served by the plan itself, it searches first (unless Settings puts Search APIs first); for any other provider's model it is never picked automatically and searches only when named in Settings.
- It is model-free: no model tokens are spent, and magpie writes no `usage.Record` and no cost for it. The vendor docs put Kimi Code and its search under the membership's shared quota, not a separate search bill.
- Deployment `10.2.1.21` serves four `kimi-code-cn/*` models and has a keyed Kimi Code (China) plan (a 5-hour + 7-day plan card). Its model list advertises `web_search: "magpie"` for those models, so the remote says it will run magpie's search for them. The search endpoint itself was not called there.

## 1. Code path

### What counts as a Kimi Code plan

`provider.KimiCodeSearch` (`internal/provider/planquota.go:378-393`):

```go
func KimiCodeSearch(p Provider) string {
	if p.Account != nil || p.Key == "" {
		return ""
	}
	for _, base := range []string{p.Chat, p.Anthropic} {
		h := hostOf(base)
		if base != "" && ((h == "api.kimi.com" || h == "api.kimi.ai") && strings.Contains(base, "/coding") || strings.HasPrefix(p.Preset, "kimi-code")) {
			return strings.TrimSuffix(kimiCodeBase(base), "/") + "/search"
		}
	}
	return ""
}
```

- Keyed only: `p.Account != nil` and `p.Key == ""` both return "". A signed-in/OAuth Kimi Code account is not used by this path.
- Recognised by host `api.kimi.com` / `api.kimi.ai` with `/coding` in the base, **or** by a preset beginning `kimi-code` (a custom base with that preset works too; `search_kimi_test.go:27` uses `http://127.0.0.1:9/coding/v1`).
- `kimiCodeBase` (`:368-376`) turns an Anthropic base ending `/coding` into `/coding/v1`; then `/search` is appended.
- The China preset (`internal/provider/presets.go:160-163`) has `Chat: https://api.kimi.com/coding/v1`, `Anthropic: https://api.kimi.com/coding`, so the service is `https://api.kimi.com/coding/v1/search`.
- The global preset (`presets.go:156-159`) gives `https://api.kimi.ai/coding/v1/search`.
- `TestKimiCodeSearchAddress` (`internal/gateway/search_kimi_test.go:20-36`) pins both, plus the "no key", Moonshot, and non-coding cases.

Upstream corroboration, kimi-cli's `auth/platforms.py` ([raw source](https://raw.githubusercontent.com/MoonshotAI/kimi-cli/main/src/kimi_cli/auth/platforms.py)):

```python
Platform(
    id=KIMI_CODE_PLATFORM_ID,
    name="Kimi Code",
    base_url=_kimi_code_base_url(),
    search_url=f"{_kimi_code_base_url()}/search",
    fetch_url=f"{_kimi_code_base_url()}/fetch",
)
```

with `_kimi_code_base_url()` defaulting to `https://api.kimi.com/coding/v1` and overridable by `KIMI_CODE_BASE_URL`.

### Request

`kimiSearch` (`internal/gateway/search_kimi.go:101-169`):

- Timeout: `context.WithTimeout(s.metered(p.Via(ctx), p, ""), searchTimeout/2)` (`:104`), i.e. 60 s (`searchTimeout, searchWaitMost = 2 * time.Minute, 5 * time.Minute`, `internal/gateway/search.go:42`). The body asks the service for `timeout_seconds: 30`.
- Query masking: `maskedQuery(query, &p)` (`:106`), so Settings → Privacy applies unless the provider is set to go unmasked (`internal/gateway/redact.go:96-109`).
- Body (`:110`):

```go
body, _ := json.Marshal(map[string]any{"text_query": q, "limit": searchHits, "enable_page_crawling": false, "timeout_seconds": 30})
```

`searchHits = 6` (`internal/gateway/search_api.go:27`).

- Headers (`:115-121`): `Content-Type: application/json`, `Accept: application/json`, `User-Agent: KimiCLI/1.52.0` (`kimiSearchAgent`, `:35`), `Authorization: Bearer <p.Key>`, and `X-Msh-Tool-Call-Id: <id>` when the search answers a model tool call.
- The call id is set by `searchRounds` (`internal/gateway/search.go:1028`): `s.webSearch(context.WithValue(ctx, searchCallKey{}, c.ID), a.Query)`.
- No `model` field and no chat request: one plain HTTP POST to the service.

This matches kimi-cli 1.52.0's `SearchWeb` ([raw source](https://raw.githubusercontent.com/MoonshotAI/kimi-cli/main/src/kimi_cli/tools/web/search.py)):

```python
session.post(
    self._base_url,
    headers={
        "User-Agent": USER_AGENT,
        "Authorization": f"Bearer {api_key}",
        "X-Msh-Tool-Call-Id": tool_call.id,
        ...
    },
    json={
        "text_query": params.query,
        "limit": params.limit,
        "enable_page_crawling": params.include_content,
        "timeout_seconds": 30,
    },
)
```

kimi-cli's `limit` defaults to 5 and is capped at 20; magpie always asks for 6 and disables page crawling.

### Response and errors

The reply is read into (`search_kimi.go:134-142`):

```go
var out struct {
	Results *[]struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
		Content string `json:"content"`
		Date    string `json:"date"`
	} `json:"search_results"`
}
```

Then (`:146-168`):

- Entries with an empty `url` are skipped.
- An empty title falls back to the URL.
- Text is `content`, else `snippet`; `searchPlain` strips markup (`search_api.go:420`) and `clipText` clamps to `pageText = 1500` (`search_api.go:30`).
- At most `searchHits` (6) pages are kept.
- No pages at all is `errFoundNothing`; `pagesSaid` (`search_api.go:187-201`) turns the pages into `"1. title — url"` lines plus the page text, and returns `[]Hit{Title, URL, PageAge}` (`internal/gateway/ir.go:85-91`).

Errors:

- Non-2xx (`:131-133`): `errors.New(p.Name + ": " + provider.APIError(b, res.Status))`.
- Bad JSON or a missing `search_results` (`:143-145`): `"unexpected reply: ..."` with `provider.APIError`.
- No answer before the 60 s deadline (`:124-126`): `"<name>: no answer in time"`.
- The response body is capped at 4 MiB (`:130`).
- `kimiSearch` does **not** retry. The outer `webSearch` tries the next searcher or search API; a searcher named in Settings is not replaced by another provider (`search.go:620-628`), and its failure is recorded for Settings (`SearcherFailed`, `search.go:665-719`).
- `searcherSpent` returns nil for a service searcher (`search.go:721-727`): "nil when it has allowance, or searches with no model (a Kimi Code plan's service)". There is no allowance pre-check.
- `metered` only applies `MaxRPM` (`internal/gateway/rpm.go:232-244`); it is not a quota read.

### Auth, account, and key selection

- Bearer is `p.Key`. `KimiCodeSearch` refuses `p.Account != nil`, so only an API key from the Kimi Code console is used, not a signed-in account.
- For a turn on one of the plan's own models, `p` is the chosen candidate: `translate` sets `searchingOn(r.Context(), p)` after the candidate is selected (`internal/gateway/gateway.go:5581-5583`), and candidates are built per key (`internal/gateway/fallback.go:175+`, `provider.WithKey` at `internal/provider/keys.go:368-384`; the candidate runs at `gateway.go:2652`). The search therefore uses the key that served the turn.
- For a searcher **named in Settings**, `modelSearch` gets `p` from `searcher()`/`Searchers()` over `provider.All()` (`search.go:360-384`), so a direct service call uses the provider's primary key, not a key picked by routing. A DeepSeek service implemented the same way inherits this limitation. (`searchWith` for model searchers loses the key differently: it re-enters `s.serve` with only `provider/model`, `search.go:784-793`, `:869`.)

### Model-free and billing

- No model is asked. The file's opening comment (`search_kimi.go:16-31`) says:

> "A Kimi Code plan searches the web with its own key, as kimi-cli's SearchWeb tool does ... No model is asked."

- `kimiSearch` is not a gateway `Call`: no `usage.Record` and no cost row. The only non-token cost carriers are `Record.OwnCost` and `Record.Credits` (`internal/usage/usage.go:195-201`), and nothing writes them for a Kimi search. `metered` only applies `MaxRPM`.
- The plan's `/usages` is read for the quota card (`internal/provider/planquota.go:395+`, `readKimiCode`), not per search.
- Vendor billing ([Membership Benefits](https://www.kimi.com/code/docs/en/kimi-code/membership.html)):

> "Kimi Code is the developer-focused AI coding service within Kimi membership benefits, provided together with a Kimi membership subscription and sharing the same quota. Requests from the CLI, VS Code, Desktop, and third-party tools all count toward that quota."

  So search is covered by the membership's shared quota; the docs do not separately price `/search`. Magpie's own comment treats it as spending the plan's allowance: naming the plan for other providers' models "would spend the plan's allowance on them" (`search_kimi.go:29-31`).

### When it searches for its own models first

`translate` pins the turn's provider and hands the search to magpie when the client's search is not native (`internal/gateway/gateway.go:5576-5583`):

```go
if !searching(r.Context()) && canSearchFor(p) && !searchesFor(p, to, model, request) {
	...
	// a Kimi Code plan's own searches stay on it
	r = r.WithContext(searchingOn(r.Context(), p))
	return s.searchReply(w, r, from, p.Name, request, u, ask)
}
```

`canSearchFor` is `searchesOwn(p) || canSearch()` (`search_kimi.go:48-50`). `searchesOwn` (`:56-61`) is:

```go
func searchesOwn(p provider.Provider) bool {
	if !searchServed() || settings.Load().Searcher == "off" {
		return false
	}
	return provider.KimiCodeSearch(p) != "" || wbSearches(p) || googleAccount(p) && searcherModel(p) != ""
}
```

`searchReply` → `webSearch` → `ownSearcher(ctx)` (`search.go:550-556`) → `ownSearch` → `kimiSearch` (`search_kimi.go:79-82`).

Two caveats:

- `webSearch` tries configured Search APIs **before** `ownSearcher` when Settings says `SearchFirst == "api"` (`search.go:542-556`). So "first" means before the searcher model, not before a user's API-first choice.
- The MCP path (`searchMCPRun`) calls `webSearch` with no `searchOnKey` (`internal/gateway/search_mcp.go:199-200`), so a Kimi plan does not serve MCP searches unless named in Settings.

`searchesOwn` also means `Searcher = "off"` or `plugins/search` off disables the plan's search even for its own models.

### When it only searches because Settings named it

`searchRank` deliberately does not rank a Kimi plan (`search.go:238-259`; it has no native search tool), and `namedSearcherRank` gives it the last, manual-only rank (`:265-279`):

```go
if provider.KimiCodeSearch(p) != "" {
	return manualRelayRank, true
}
```

`autoSearcher` therefore never picks it. The code comment (`search_kimi.go:29-31`):

> "For other providers' models a plan searches only when Settings names it: picking it by itself would spend the plan's allowance on them, and change the searcher users have now."

When named, `chosenSearcher` returns it with no model (`search.go:341-343`):

```go
case provider.KimiCodeSearch(p) != "":
	// it searches with no model
	return &p, "", ""
```

`modelSearch` then calls `searchBy` (`search.go:618`), which routes to `kimiSearch` (`search_kimi.go:93-98`). The named searcher is not replaced by another provider (`search.go:620-628`); `Searchers()` skips manual-only/service choices in the fallback loop (`search.go:631-634`).

### Settings presentation

- `Searchers()` appends `SearcherChoice{Provider: p, rank: r, ManualOnly: manualOnly, Service: true}` with no `Small` and no `Models` (`search.go:367-369`).
- `SearcherChoice.Service` is documented as: "Service is a Kimi Code plan, which searches by its search service, with no model (and no Small or Models)" (`search.go:393-395`).
- `searchState` passes `Service` through (`internal/gui/api.go:557-566`).
- `app.js` renders it as `"<name> · its web search"` (`internal/gui/assets/app.js:21029`), saves just the provider id as the option value (`:21077-21079`), and adds the note "A Kimi Code plan (...) searches for its own models first, with its web search; for other models only when named here" (`:21051-21053`).
- Settings validation requires the name to be in `gateway.Searchers()` (`internal/gui/api.go:1268-1274`, per `search-fetch-seams.md`).

## 2. Deployment `10.2.1.21`

Everything below is a read-only GET. The `bi-dev` key was read from `~/.config/magpie/providers.json` into a shell variable and sent as a bearer; it was never printed.

Requests made:

- `GET http://10.2.1.21:3425/api/hello` → `{"name":"magpie","version":"v0.1.1279-fork.96110762"}`.
- `GET http://10.2.1.21:3425/v1/models` → HTTP 200, 28 entries, `has_more: false`, no pagination.
- `GET /v1/models` with `X-Magpie-Drawers: 1` → each model gains a `web_search` field.
- `GET /v1/magpie/quotas?kept=1` → 7 cards from the kept snapshot; the handler serves kept data without asking any vendor (`internal/gateway/gateway.go:729-733`).

### What it serves for `kimi-code-cn`

| id | display_name | context_length | max_output_tokens | native_endpoints | `web_search` |
| --- | --- | --- | --- | --- | --- |
| `kimi-code-cn/kimi-for-coding` | K2.8 Preview | 1,048,576 | 32,768 | `/v1/chat/completions`, `/v1/messages` | `magpie` |
| `kimi-code-cn/kimi-for-coding-highspeed` | K2.7 Code Highspeed | 262,144 | 32,768 | `/v1/chat/completions`, `/v1/messages` | `magpie` |
| `kimi-code-cn/k3` | K3 | 1,048,576 | 131,072 | `/v1/chat/completions`, `/v1/messages` | `magpie` |
| `kimi-code-cn/k3-256k` | K3-256k | 262,144 | 131,072 | `/v1/chat/completions`, `/v1/messages` | `magpie` |

All four report `owned_by: "kimi-code-cn"`, `object: "model"`, `type: "model"`. The deployment also lists `group/auto-kimi-k3` (routing group, `web_search: "magpie"`) and `uco/kimi-k3` (`owned_by: uco`, `web_search: "magpie"`).

Without `X-Magpie-Drawers`, the entries have no `web_search` field. With it, every model gets one; `zhipu/*` entries say `native` while the Kimi entries say `magpie`. The field is produced by `webSearchOf` (`internal/gateway/search_remote.go:66-84`) and written in the models handler (`gateway.go:1052-1055`). Note that `gateway.go:1016-1017` computes `magpie := header != ""` and `searches := magpie && canSearch()`, then passes `searches` to `webSearchOf`, whose parameter is also named `magpie`. When `canSearch()` is true, every model gets `"magpie"`; when it is false, only a provider whose `searchesOwn(p)` is true gets it (for a Kimi entry, a keyed plan with search enabled). So the field proves `searchServed()` and a searcher or the plan's own service, not which searcher.

`web_searchOf` for these models (`search_remote.go:68-83`):

```go
if magpie || searchesOwn(p) {
	return searchMagpie
}
```

Thus `"magpie"` says the remote will run its own search for these models; it does not name which searcher.

### The plan behind it

The kept quota snapshot has a `kimi-code-cn` card with `kind: "plan"` and windows `5 hours` and `7 days` (no user, no balance). `PlanQuotas` only builds cards for providers with `p.Hidden == false`, `p.Off == false`, `p.Account == nil`, and `p.Key != ""` (`internal/provider/planquota.go:652-655`), and Kimi's source is `/coding/v1/usages` (`:45-48`, `readKimiCode` at `:395+`). So the deployment has a **keyed** Kimi Code (China) plan, not merely a signed-in account.

Consequences:

- `provider.KimiCodeSearch` is non-empty for that provider.
- The deployment's model list marks the four Kimi models `web_search: "magpie"`. That shows `searchServed()` is on (`plugins/search`), and that either the deployment has a searcher (`canSearch()`) or the Kimi plan declares that it searches for its own models (`searchesOwn`). It does not by itself show the global `Searcher` setting.
- If `Searcher != "off"` (not readable from the model list), `searchesOwn(p)` is true for those models, so a request there for one of them should reach `https://api.kimi.com/coding/v1/search` first (unless Settings sets Search APIs first).
- The global `Searcher` setting could still be another provider, or `off`; the model list cannot tell. If it is `off`, `searchesOwn` is false for the Kimi models and only configured Search APIs run.

### What is not evidenced

The remote's `/coding/v1/search` was **not called**: that would spend the plan's quota. The evidence that it is available is the keyed plan card plus the provider's `web_search: "magpie"` mark; whether it actually runs depends on `Searcher != "off"`, which the metadata does not expose. No chat, search, or write request was sent to `10.2.1.21`.

### What this means locally

`bi-dev` in `~/.config/magpie/providers.json` is a `remote-magpie` preset. For a Remote magpie, `searchableModel` requires the remote entry's `web_search` to be `"native"` (`internal/gateway/search_remote.go:53-64`); the four Kimi entries are `"magpie"`, so this local magpie cannot use them as a searcher. Only the `zhipu/*` entries marked `native` are candidates from here.

## 3. Comparison for DeepSeek

### The shape that would make DeepSeek "provider-linked like Kimi Code"

A **service searcher**, not a model searcher:

1. A predicate beside `KimiCodeSearch`, say `DeepSeekSearch(p)`, returning `https://api.deepseek.com/v1/tools/search` when the provider is DeepSeek (host/preset) with `p.Account == nil && p.Key != ""`.
2. A direct HTTP caller beside `kimiSearch`, using `Authorization: Bearer p.Key`, no `model`, and `provider.APIError` for non-2xx.
3. Wiring into `ownSearcher`/`ownSearch`/`searchBy` (`search_kimi.go:73-98`) so Settings and the own-model path can reach it.
4. Inclusion in `searchesOwn` (`:56-61`) if it should search for DeepSeek's own models first, or only in `namedSearcherRank`/`Searchers` if it should be manual-only.
5. A no-model branch in `chosenSearcher` (`search.go:341-343`).
6. The `searcherSpent` no-model escape (`search.go:721-727`).
7. A `Searchers()` entry with `Service: true`, `ManualOnly: true`, no `Small`, no `Models` (`search.go:367-369`). The GUI then reuses the "its web search" label and provider-only option automatically (`app.js:21029`, `21077-21079`), and Settings validation accepts it.

The request/response mapping:

- Request: `{"query": q, "count": searchHits}` (6), optionally `need_content: true` if the page text is wanted; `localization` and `ori_question` are optional.
- Response: `{"results":[{rerank_score,title,url,snippet,summary,content,site_name,site_icon,date,authority}]}`.
- Map to `foundPage{Title, URL, Text, Age}`: title, URL, `date` → `Age`; `Text` = `content` when `need_content` and non-empty, else `summary` (600-800 chars, written for a model), else `snippet` (100-200 chars); clip to `pageText` (1500); cap at `searchHits`.
- `Hit` only carries `Title`, `URL`, `PageAge` (`internal/gateway/ir.go:85-91`), so `rerank_score`, `authority`, `site_name`, and `site_icon` are dropped unless that type is extended.
- Errors: OpenAI-shaped; 400/401/402/403/404/408/413/429/500/504 per the DeepSeek docs. 402 is "insufficient balance", 429 is QPS exceeded, 408/5xx are retryable; the current Kimi caller retries none and lets the outer fallback handle it.
- Timeout: `searchTimeout/2` (60 s) is available; DeepSeek documents 408/504, so the same 60 s cap is reasonable.
- Key selection: on the own-model path, `searchOnKey` already pins the serving candidate (and therefore its key). On the named-searcher path, `p.Key` is the primary key, as with Kimi today; choosing among `p.Keys` would need a new mechanism (`provider.WithKey`, `keys.go:368-384`).

### What transfers

- The entire service-searcher seam: predicate, direct keyed call, `ownSearch`/`searchBy`, `Service` UI, no-model Settings choice, no separate credential store.
- Privacy masking via `maskedQuery`, because it is called before the request (`search_kimi.go:106`).
- `pagesSaid`/`Hit` conversion, so agents see the pages and `K_search` events.
- The `searcherSpent` no-model escape and the rule that a named searcher is not silently replaced.
- Model-free operation: neither service spends model tokens; no model is asked.

### What does not transfer

- **Billing model.** Kimi's search is part of a membership's shared quota, and magpie records no cost. DeepSeek's Search is billed per successful request — $0.75 / 5元 per 1,000 successful requests; failures, empty results, and timeouts are not billed; `count` does not change the price. If magpie does not record it, the spend is invisible. `usage.Record.OwnCost` and `Credits` (`usage.go:195-201`) exist, and `Kind: "web_search"` already marks a search call (`codex_backend.go:1236`), but nothing writes a search-API cost today.
- **Quota source and rate limits.** Kimi's card comes from `/coding/v1/usages` (`planquota.go:45-48`). DeepSeek has no plan card; its limits are 5 QPS for Search and 5 QPS for Fetch, with 429 when exceeded. `metered`/`MaxRPM` is a local RPM setting, not the vendor's QPS; 429 handling must be explicit.
- **Auto-first rationale.** Kimi's own-models-first rule exists so "a Kimi user's searches stay on Kimi" and the plan's allowance is used. DeepSeek's search is real money per request, so auto-preferring it for DeepSeek's own models would spend money unasked. The safer shape is manual-only for all models, or an explicit opt-in; the Kimi comment's caution ("picking it by itself would spend the plan's allowance on them") applies more strongly to per-request billing.
- **Native search overlap.** DeepSeek already searches natively on its Anthropic and Responses endpoints (`searchHosts`, `search.go:202-207`; tickets 02/03). A DeepSeek service searcher is mainly for *other* models, or for protocols where the native tool is not used. Kimi has no native search tool, so its service is the only search route for its models.
- **Fetch.** DeepSeek has `/v1/tools/fetch`; kimi-cli also exposes `fetch_url = <base>/fetch` (`platforms.py`), but magpie has no provider-linked fetch path at all — `web_fetch` has no provider/account hook (`search_fetch.go`, and `search-fetch-seams.md`). So there is no Kimi fetch implementation in magpie to copy; DeepSeek fetch needs a new backend seam, not another service-searcher variant.
- **Plan vs pay-as-you-go detection.** `KimiCodeSearch` keys off the `/coding` host/path or the `kimi-code` preset. DeepSeek's endpoint is the same for every key, so `DeepSeekSearch` can be a simple host/preset + key check; there is no plan-only key class.
- **Search-first placement.** `webSearch` order is API-first (if set) → own → model → API (`search.go:525-574`). A DeepSeek service can occupy the `ownSearcher` slot or only `modelSearch`; that is a product choice, not a code constraint.
- **Result fields and quality.** DeepSeek returns `rerank_score` (0-3; ≥1.5 high quality), `authority` (0-4), `summary`, `site_name`, and `site_icon`; Kimi returns `site_name`, `icon`, `mime`, `snippet`, `content`, `date`. Magpie keeps only title/URL/age as `Hit`, so neither's extra fields survive to the client unless the IR is extended.

## Not verified

- The remote deployment's `/coding/v1/search` was not called. That the service is enabled there is inferred from the keyed plan card and `web_search: "magpie"`, not from a live search.
- The deployment's global `Searcher` setting is unknown. For models of other providers, `web_search: "magpie"` can come from `canSearch()` alone, so it does not prove the Kimi plan is the searcher for them.
- Whether the remote's `kimi-code-cn` provider uses the `kimi-code-cn` preset or a custom base; only the model entries (`owned_by: kimi-code-cn`) and the quota card are visible. Either way `KimiCodeSearch` matches the preset or the `/coding` host.
- Whether Kimi Code's `/search` is separately metered. The vendor docs say Kimi Code shares the membership quota and that CLI/third-party requests count; they do not publish a per-search price. Magpie records no cost either way.
- Whether search consumes the 5-hour/window quota differently from model calls. The docs say requests count; no finer breakdown was found.
- Whether `kimiSearch` should retry 429/5xx. The current code does not.
- Whether DeepSeek's `count` and `need_content` affect latency only or also the price. The docs say the price is per successful request and `count` does not affect it; content size may still affect latency.
- Whether `web_search: "magpie"` on `group/auto-kimi-k3` and `uco/kimi-k3` routes to the Kimi plan or another searcher.
- DeepSeek's exact error body for `/v1/tools/search` on a custom relay base; the docs describe the platform endpoint only.

## Sources

- Code: `internal/provider/planquota.go`, `internal/provider/presets.go`, `internal/gateway/search_kimi.go`, `internal/gateway/search.go`, `internal/gateway/search_api.go`, `internal/gateway/search_remote.go`, `internal/gateway/search_mcp.go`, `internal/gateway/gateway.go`, `internal/gateway/rpm.go`, `internal/gateway/fallback.go`, `internal/provider/keys.go`, `internal/usage/usage.go`, `internal/gui/api.go`, `internal/gui/assets/app.js`, `internal/gateway/search_kimi_test.go`.
- Tests: `TestKimiCodeSearchAddress`, `TestKimiCodePlanSearchesForItsModels`, `TestKimiCodePlanNamedToSearch` (`internal/gateway/search_kimi_test.go`), `TestSearchProviderOffTellsRemoteMagpie` (`internal/gateway/search_off_remote_test.go`).
- kimi-cli: [search.py](https://raw.githubusercontent.com/MoonshotAI/kimi-cli/main/src/kimi_cli/tools/web/search.py), [platforms.py](https://raw.githubusercontent.com/MoonshotAI/kimi-cli/main/src/kimi_cli/auth/platforms.py).
- Kimi Code docs: [Membership Benefits](https://www.kimi.com/code/docs/en/kimi-code/membership.html), [FAQ](https://www.kimi.com/en/help/kimi-code/faq).
- DeepSeek docs (from `deepseek-search-fetch-api.md`): [Search](https://api-docs.deepseek.com/api/search), [Fetch](https://api-docs.deepseek.com/api/fetch), [pricing](https://api-docs.deepseek.com/quick_start/pricing), [rate limits](https://api-docs.deepseek.com/quick_start/rate_limit).
- Deployment: read-only GETs to `http://10.2.1.21:3425/api/hello`, `/v1/models`, `/v1/models` with `X-Magpie-Drawers: 1`, and `/v1/magpie/quotas?kept=1`.

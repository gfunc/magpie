# Can providers give magpie MCP servers or tools?

Read-only exploration of `/home/jason/projects/magpie` (fork tip `4fc49822`),
2026-10-17. Companion to [magpie's search and web_fetch seams](search-fetch-seams.md)
and [Kimi Code (China)'s search service](kimi-code-cn-search.md).

## Gist

- MCP is **one-way**: magpie serves three MCP servers to agents of its own
  (`/mcp/magpie/web-search`, `/mcp/magpie/vision`, `/mcp/magpie/tools`) and
  proxies a fourth kind, `/mcp/{name}`, to a remote server the user signed
  in to through the Library. No provider, provider plugin, or agent plugin
  supplies magpie an MCP server or a tool.
- A provider plugin can give agents tools — but only through OpenCode's
  `tool` hook, served at `/mcp/magpie/tools`. That path is independent of
  the provider's chat API and of magpie's own `web_search`/`web_fetch`.
- Provider plugins have no hook for "search the web for this query" or
  "fetch this page". `searches: true` only means the vendor runs a hosted
  `web_search` tool itself on Anthropic/Responses.
- DeepSeek's `/v1/tools/search` and `/v1/tools/fetch` therefore cannot be
  delivered by a provider plugin alone. They need core code: a service
  searcher beside `provider.KimiCodeSearch`, and a fetch backend seam.

## 1. MCP direction

### What magpie serves to agents

| Path | What it is | Who serves it | Source |
| --- | --- | --- | --- |
| `/mcp/magpie/web-search` | magpie's own search server (Streamable HTTP, POST JSON-RPC). Two tools: `web_search` and `web_fetch`. | the gateway; the tool point `gwtool.WebSearch`, served by the built-in plugin `plugins/search` | [`SearchMCPPath`](../../../internal/gateway/search_mcp.go), [`gateway.go:678`](../../../internal/gateway/gateway.go), [`plugins/search/search.go`](../../../plugins/search/search.go) |
| `/mcp/magpie/vision` | magpie's own image-recognition server, one `analyze_image` tool. | the gateway; gated by the vision settings, each call reserves the calling key's limit | [`VisionMCPPath`](../../../internal/gateway/vision_mcp.go), [`gateway.go:679`](../../../internal/gateway/gateway.go) |
| `/mcp/magpie/tools` | every on plugin's `tool` hook, named `<plugin>_<tool>`. | the gateway; the tool point `gwtool.PluginTools`, served by the built-in plugin `plugins/tools` | [`ToolsMCPPath`](../../../internal/gateway/tools_mcp.go), [`gateway.go:680`](../../../internal/gateway/gateway.go) |
| `/mcp/{name}` | **not a server of magpie's own**: a relay to a remote MCP server the user signed in to once in the Library. | the gateway's MCP client/proxy, using tokens from `internal/mcpauth` | [`mcpProxy`](../../../internal/gateway/mcp.go), [`gateway.go:677`](../../../internal/gateway/gateway.go) |

The three own servers are guarded by `serveMCP`: another machine needs
magpie's LAN sharing and a gateway key, a web page's `Origin` is refused,
and each call reserves the calling gateway key's budget
([`search_mcp.go:52`](../../../internal/gateway/search_mcp.go)). Their
addresses are listed in `/api.json`
([`gateway.go:708`](../../../internal/gateway/gateway.go)).

`web_fetch` is today a **second tool on the web-search server**, not a
separate server: `tools/list` returns `web_search` and `fetchTool()`
together ([`search_mcp.go:130`](../../../internal/gateway/search_mcp.go)),
and `tools/call` dispatches both
([`search_mcp.go:137`](../../../internal/gateway/search_mcp.go)). The
destination's `/mcp/magpie/web-fetch` does not exist yet.

`/_magpie/claude-mcp/<token>` is a different thing: the Claude
subscription bridge's own helper calling back with a tool result
([`claude_subscription.go:580`](../../../internal/gateway/claude_subscription.go),
[`gateway.go:676`](../../../internal/gateway/gateway.go)). It is not
provider-supplied MCP.

### Does any provider supply MCP or tools to magpie?

**No.** There is no code path by which a provider, provider plugin, or
agent plugin hands magpie an MCP server, an MCP endpoint, or a tool that
magpie then serves.

- The provider-plugin hook list is `config`, `auth` (methods, loader,
  usage, refresh, checkin, options), `provider.models`, `chat.headers`,
  and `tool` ([`host.js:1198`](../../../internal/plugin/host.js),
  [`host.js:1384`](../../../internal/plugin/host.js),
  [`host.js:2025`](../../../internal/plugin/host.js)). None returns an
  MCP server. `host.js` mentions MCP only to say an MCP server is not a
  plugin ([`host.js:1107`](../../../internal/plugin/host.js)).
- An npm package that is only an MCP server is refused as a plugin with
  "add it under Library → MCP servers instead"
  ([`github.go:373`](../../../internal/plugin/github.go),
  `TestCommandOnlyPackageIsNoPlugin` in
  [`notplugin_test.go:23`](../../../internal/plugin/notplugin_test.go)).
- The only MCP client in magpie is the Library's: `internal/mcpauth` signs
  in to a remote server and `/mcp/{name}` relays to it. Its records come
  from the user's Library, never from a provider
  ([`mcpauth.go:1`](../../../internal/mcpauth/mcpauth.go),
  [`library.md`](../../../docs/subsystems/library.md) "MCP servers").
- Agent plugins (`internal/agentplug`) can name an **MCP file the agent
  reads** (`agent.library.mcp` plus `mcpFormat`), and magpie writes the
  Library's servers into it. That is magpie → agent, not plugin → magpie
  ([`agentplug.go:27`](../../../internal/agentplug/agentplug.go)).
- The newer `magpie.hooks` gateway packages cannot register tools either:
  "It cannot reject requests or register tools"
  ([`gateway-request-hooks.md:38`](../../../docs/subsystems/gateway-request-hooks.md)).
- A provider plugin's `tool` hook is the one place a plugin's tools reach
  agents, and the gateway owns that server; the plugin does not supply the
  server itself (see §2).

### `/mcp/{name}`: who configures it

It is a **signed-in remote MCP server proxy**, not a Library MCP server
definition and not a plugin server. The flow:

1. The user adds an MCP server (command or URL) in **Library → MCP
   servers**; it is kept in `library.json`'s `mcp`
   ([`library.md`](../../../docs/subsystems/library.md)).
2. A server that wants OAuth is signed in once in magpie. `mcpauth.Start`
   discovers the authorization server, registers magpie (or uses the
   user's own OAuth app), runs the browser + PKCE flow, and keeps the
   tokens in `mcp-signins.json`
   ([`mcpauth.go:803`](../../../internal/mcpauth/mcpauth.go),
   [`mcpauth.go:119`](../../../internal/mcpauth/mcpauth.go)).
3. The Library gives each agent magpie's address,
   `http://127.0.0.1:3425/mcp/<name>`, in place of the server's own
   ([`library.md`](../../../docs/subsystems/library.md)).
4. At request time `mcpProxy` looks the name up with `mcpauth.Get`, refuses
   when magpie is not signed in ("sign in to it in magpie's Library"),
   renews on a 401 once, and streams the server's answer back
   ([`mcp.go:37`](../../../internal/gateway/mcp.go)).

So the configurers are the Library page (server list) and the sign-in
flow; providers have no part in it.

## 2. Provider extension mechanisms

"Provider extension" is **not a term the repo uses**. The documented names
are "provider plugins" (OpenCode `auth` hook / pi packages) and "gateway
extension points" (the built-in `pluginrt` plugins). `market.json` provider
entries carry no `kind`; `middleware`, `tools`, `page` and `agent` are the
kinds ([`market.go:45`](../../../internal/plugin/market.go)).

| Seam | Where it lives | What it can add | What it cannot add |
| --- | --- | --- | --- |
| Built-in preset | `internal/provider/presets.go` | a vendor's base URLs, catalog, model rules; core can special-case it (Kimi Code's search) | anything without a code change |
| Custom provider | `providers.json` entry with key + base URLs | keys, URLs, headers, `Searches`, extra body, balance URL, routing settings | a new capability; no code runs |
| OpenCode / pi provider plugin | Bun host (`internal/plugin`, `internal/provider/plugins.go`) | sign-in, token refresh, model lists, usage, check-in, options, request fetch/headers, models' tiers/decision marks, `searches` pass-through, and agent tools | a searcher/fetcher magpie calls; an MCP server; cost accounting |
| Built-in Go plugin (`pluginrt`) | `plugins/`, `internal/pluginrt` | gateway extension points: observer, router strategy, middleware, tool, budget | a provider's behavior; the tool point serves only the fixed names `web_search` and `plugin_tools` |
| Community JS gateway plugin | `internal/middleware`, Bun host | `magpie.middleware`: request/reply middleware, `strategies`, `onUsage`; `magpie.hooks`: `onRequest` context enrichment, `onResponse` observation | tools, MCP servers, searcher backends |
| Library MCP server | `library.json`, `internal/mcpauth` | an MCP server for agents, optionally via `/mcp/{name}` | anything to magpie itself |

### What a provider plugin can do

The host's contract, from `host.js`:

- **`config`** declares the provider and default models; magpie's own
  fields on it are `searches` and `decide`
  ([`host.js:1619`](../../../internal/plugin/host.js),
  [`host.js:1636`](../../../internal/plugin/host.js)):
  > `searches: cfg?.searches === true,` — "the provider's upstream answers
  > a web search tool offered on its Anthropic or Responses API by itself,
  > so a client's web_search goes to it as sent (#2027)".
- **`auth`** provides sign-in `methods`, a `loader`, `usage`, `refresh`
  and `refreshLead`, `checkin`, `options`, `icon`, `maxConcurrency`
  ([`providers.go:109`](../../../internal/plugin/providers.go),
  [`host.js:2025`](../../../internal/plugin/host.js),
  [`host.js:1764`](../../../internal/plugin/host.js)).
- **`auth.loader`** returns OpenCode's options — `baseURL`, `apiKey`,
  `headers`, and an optional `fetch`:
  > `opts = (await inScope(provider, key, () => a.loader(async () => readAuth()[key], …))) ?? {}`
  ([`host.js:2119`](../../../internal/plugin/host.js)).
- **`provider.models`** lists the models one account has
  ([`host.js:1667`](../../../internal/plugin/host.js)); **`chat.headers`**
  adds per-request headers ([`host.js:2309`](../../../internal/plugin/host.js)).
- Magpie maps the result into a `provider.Provider`: `pluginProvider` sets
  `p.Searches = pp.Searches` and builds the plugin:// base URLs
  ([`plugins.go:332`](../../../internal/provider/plugins.go),
  [`plugins.go:412`](../../../internal/provider/plugins.go)).
- Every request is sent through the plugin: `pluginFetch` builds the URL
  and calls `plugin.Fetch`, which the host runs via `fetchAs` with
  `o.fetch` or Bun's `fetch`
  ([`plugins.go:457`](../../../internal/provider/plugins.go),
  [`host.go:1599`](../../../internal/plugin/host.go),
  [`host.js:2294`](../../../internal/plugin/host.js)).

So "fetch" is a **transport for provider API requests**, not a general
capability magpie can ask the plugin to perform. `Provider.Do` is the only
core entry that goes through the same transport
([`plugins.go:641`](../../../internal/provider/plugins.go)); the search and
fetch code never calls it.

### Can a provider plugin give tools agents see?

Yes, but only through the plugin-tools path, and separately from being a
provider:

- OpenCode's `tool` hook, `{tool: {name: tool({description, args, execute})}}`,
  is collected from every loaded plugin
  ([`host.js:1374`](../../../internal/plugin/host.js),
  [`toolsOf` at `host.js:1384`](../../../internal/plugin/host.js)).
- `plugins/tools` serves `gwtool.PluginTools`; the gateway exposes the
  tools at `/mcp/magpie/tools`, each named `<plugin>_<tool>`, each call
  held to the calling key's limit and run in the host with a 55 s timeout
  ([`tools_mcp.go:31`](../../../internal/gateway/tools_mcp.go),
  [`tools_mcp.go:145`](../../../internal/gateway/tools_mcp.go),
  [`tools_mcp.go:205`](../../../internal/gateway/tools_mcp.go),
  [`plugin-tools.md`](../../../docs/subsystems/plugin-tools.md)).
- The tool call's context has no account or credential: `ctx` is
  OpenCode's `{sessionID, messageID, callID, agent, directory, worktree,
  abort, metadata}` ([`host.js:1509`](../../../internal/plugin/host.js)).
  A tool that needs its provider's key must capture it when a loader or
  auth hook runs; there is no `getAuth` passed to tools, and
  `client.auth` only sets/removes a sign-in
  ([`host.js:1007`](../../../internal/plugin/host.js)).
- Tools do not flow through the provider's model API and are not part of
  `web_search`/`web_fetch`: the agent must mount `/mcp/magpie/tools` and
  call them itself.

One documentation inconsistency: the provider-plugin guide still says
"The hooks magpie calls are `config`, `auth` …, `provider.models` and
`chat.headers`. A plugin's other hooks (`tool`, …) are not called"
([`plugins.html:555`](../../../site/public/docs/plugins.html)), while the
same page documents the tool hook and `/mcp/magpie/tools` at
[`plugins.html:716`](../../../site/public/docs/plugins.html), and the code
does call it. The code and `plugin-tools.md` are the accurate ones; the
sentence at line 555 looks stale.

### Where it is documented

- [`AGENTS.md:25`](../../../AGENTS.md) — the moved-built-in table and
  "Which code actually runs".
- [`provider-plugins.md`](../../../docs/subsystems/provider-plugins.md) —
  ownership, hooks, and "A plugin whose upstream searches" (line 63).
- [`plugin-runtime.md`](../../../docs/subsystems/plugin-runtime.md) — the
  built-in `pluginrt` extension points, including the tool point (line 221)
  and P4 community plugins (line 252).
- [`plugin-tools.md`](../../../docs/subsystems/plugin-tools.md) — the
  `tool` hook and `/mcp/magpie/tools`.
- [`providers-accounts.md`](../../../docs/subsystems/providers-accounts.md) —
  the `Provider` fields, including `Searches`.
- [`reference.md:862`](../../../docs/reference.md) — the user-facing
  "Plugins" section.
- [`integrating.md:140`](../../../docs/integrating.md) — the two kinds,
  provider plugins and gateway middleware.
- [`plugins.html`](../../../site/public/docs/plugins.html) — the plugin
  author guide.

## 3. DeepSeek fit

### Could `/v1/tools/search` and `/v1/tools/fetch` be a provider extension?

**Not as a substitute for core code.** A provider plugin can present
DeepSeek as a provider (key, base URLs, models, usage) and can optionally
register agent tools, but it cannot make magpie's own search or fetch use
DeepSeek's tools APIs.

What a plugin could declare today:

```js
export const DeepSeek = async () => ({
  config: async (cfg) => {
    cfg.provider.deepseek ??= {
      name: "DeepSeek",
      npm: "@ai-sdk/openai-compatible",
      api: "https://api.deepseek.com/v1",
      models: { "deepseek-v4-pro": { name: "DeepSeek V4 Pro" } },
    }
  },
  auth: {
    provider: "deepseek",
    methods: [{ type: "api", label: "API key" }],
    loader: async (getAuth) => ({ apiKey: (await getAuth()).key }),
    usage: async () => ({ /* a card, if DeepSeek told one */ }),
  },
  tool: {
    deepseek_search: tool({ description: "…", args: { query: tool.schema.string() }, async execute({ query }) { /* POST /v1/tools/search */ } }),
    deepseek_fetch:  tool({ description: "…", args: { url: tool.schema.string() },   async execute({ url })   { /* POST /v1/tools/fetch */ } }),
  },
})
```

With that, agents that mount `/mcp/magpie/tools` could call
`deepseek_search` / `deepseek_fetch`. Magpie itself would not call them,
and Settings would not list them.

What a plugin **cannot** do today:

- Be named as a searcher in Settings → Web search. `Searchers()` only adds
  a model searcher, or the hard-coded service case
  `provider.KimiCodeSearch(p) != ""`
  ([`search.go:367`](../../../internal/gateway/search.go)).
- Answer magpie's `web_search` stand-in for a model that cannot search.
  `webSearch` reaches `ownSearcher` / `modelSearch` / `apiSearch`
  ([`search.go:525`](../../../internal/gateway/search.go)); none consults a
  plugin hook.
- Back `web_fetch`. `fetchMCPRun` always calls `fetchPage`, whose client is
  the fixed `webFetchClient = publicClient`
  ([`search_fetch.go:66`](../../../internal/gateway/search_fetch.go),
  [`search_fetch.go:126`](../../../internal/gateway/search_fetch.go)).
- Report per-request cost. Search API and Kimi/WorkBuddy service calls
  record no usage row and no cost; plugin tools are not usage records
  either.
- Use `searches: true` for this. That flag means the vendor's own hosted
  `web_search` tool, offered on Anthropic/Responses, and only makes a
  client's tool pass through
  ([`search.go:100`](../../../internal/gateway/search.go),
  [`provider-plugins.md:63`](../../../docs/subsystems/provider-plugins.md)).
  DeepSeek's standalone tools are plain HTTP APIs, not a hosted model tool;
  its Responses `web_search` works only on `deepseek-v4-pro` and is ignored
  on `deepseek-flash`, and MCP is listed as ignored
  ([DeepSeek API notes](deepseek-search-fetch-api.md)).

### The core seam it would reuse: Kimi Code

`provider.KimiCodeSearch` is a **core** predicate over a provider's own
key and base URL:

```go
func KimiCodeSearch(p Provider) string {
	if p.Account != nil || p.Key == "" {
		return ""
	}
	…
	return strings.TrimSuffix(kimiCodeBase(base), "/") + "/search"
}
```
([`planquota.go:382`](../../../internal/provider/planquota.go)).

`kimiSearch` then makes a direct keyed HTTP call, with query masking and
the provider's proxy/meter
([`search_kimi.go:101`](../../../internal/gateway/search_kimi.go)); it is
wired into `ownSearcher`/`ownSearch`/`searchBy`
([`search_kimi.go:73`](../../../internal/gateway/search_kimi.go)), a
no-model branch in `chosenSearcher` (`search.go:341`), the `Service` entry
in `Searchers()` (`search.go:367`), and the GUI's "its web search" label.

What core code has that a provider plugin lacks:

| Core has | Plugin has |
| --- | --- |
| a searcher predicate and a direct vendor call keyed by the provider's key | no searcher hook |
| `webSearch` ordering, `searchReply`, `K_search` events, `Hit` conversion | a tool result sent only to the calling agent |
| `maskedQuery` and restore, `metered`/proxy of the provider | its own fetch, no masking contract |
| Settings' searcher list, `searchState`, the "its web search" UI | nothing in Settings → Web search |
| turn-aware `ownSearcher` with the candidate's key (`searchOnKey`) | no turn or account context |
| `fetchPage`'s public-address guard, Markdown conversion, MCP server | an agent-invoked tool only |

### Conclusion

DeepSeek search/fetch is a **core change**, not a provider plugin:

1. Add a `DeepSeekSearch(p)` predicate and a direct `/v1/tools/search`
   caller beside the Kimi seam, and wire it into `searchesOwn` /
   `namedSearcherRank` / `chosenSearcher` / `Searchers()` as a manual-only
   `Service` (per ticket 04's decision).
2. Add a fetch backend seam behind `web_fetch` (none exists today), with
   DeepSeek `/v1/tools/fetch` opt-in and the current downloader as the
   default.
3. Key selection and cost/429 handling need explicit work; neither the
   Kimi caller nor the plugin tool path provides them.

A provider plugin could be an *additional* delivery channel (agent tools
`deepseek_search` / `deepseek_fetch`), but it would duplicate the feature,
not implement the destination, and it would depend on the loader having
captured the key because tools get no auth context.

## Not verified

- No tests were run, and no live DeepSeek, Kimi, or MCP call was made.
- Whether `/mcp/magpie/vision` is gated by a `pluginrt` tool point: it is
  not in `gwtool` (only `WebSearch` and `PluginTools` are), and its
  availability follows the vision settings instead. Read from code only.
- The exact release history behind the stale `plugins.html:555` sentence;
  the code's `tool` hook and `plugin-tools.md` are assumed to win.
- Whether a `tool` hook can reliably obtain a provider's key in every
  plugin shape (loader capture works for a provider request, but no
  documented way exists for a tool to call `getAuth`); not exercised.
- Whether a plugin tool could call its vendor through the provider's own
  `fetch`/proxy setup without duplicating credentials; not exercised.
- Whether any third-party (community) plugin supplies an MCP server by
  other means (for example, a tool that itself proxies an MCP server).
  Repo code has no such path.
- DeepSeek's exact error bodies on a custom relay base, and whether its
  `/v1/tools/*` endpoints are present on every preset URL (the preset uses
  `api.deepseek.com/v1` and `/anthropic`; the tools docs name
  `api.deepseek.com/v1/tools/...`).
- Cost accounting for a DeepSeek search/fetch call: no existing record
  path writes a per-request search cost.

## Sources

- Code: `internal/gateway/{mcp.go,search_mcp.go,search_fetch.go,vision_mcp.go,tools_mcp.go,gateway.go,search.go,search_kimi.go}`,
  `internal/mcpauth/mcpauth.go`, `internal/library/{library.go,mcp.go}`,
  `internal/plugin/{host.js,host.go,providers.go,tools.go,github.go,market.go}`,
  `internal/provider/{plugins.go,planquota.go,presets.go,provider.go}`,
  `internal/gwtool/gwtool.go`, `internal/agentplug/agentplug.go`,
  `plugins/{search,tools}`.
- Docs: `AGENTS.md`, `docs/subsystems/{provider-plugins,plugin-runtime,plugin-tools,providers-accounts,gateway-routing,library,gateway-request-hooks}.md`,
  `docs/reference.md`, `docs/integrating.md`, `site/public/docs/plugins.html`.
- Prior research: [search-fetch-seams.md](search-fetch-seams.md),
  [kimi-code-cn-search.md](kimi-code-cn-search.md),
  [deepseek-search-fetch-api.md](deepseek-search-fetch-api.md).

# Map whether providers can give magpie MCP servers or tools

Status: resolved
Type: research

## Question

Can an existing or future provider give magpie an MCP server or tools, and what provider-extension seams already exist?

- **Direction:** magpie serves MCP to agents (`/mcp/magpie/web-search`, `/mcp/magpie/vision`, plugin tools). Does any provider instead supply an MCP server *to* magpie?
- **Provider plugins:** how `internal/plugin` runs OpenCode provider plugins, what hooks they expose (`providers.go`, config), and whether they can register tools or MCP.
- **Plugin tools:** `internal/plugin/tools.go`, `internal/gateway/tools_mcp.go`, `internal/gwtool`, `internal/pluginrt` — who can add tools, and how.
- **Signed-in MCP proxy:** `/mcp/{name}` and `internal/library/mcp.go` — magpie as MCP client/proxy; is this provider-related?
- **Provider extension seams:** built-in presets, custom key+URL providers, OpenCode plugins, `Provider.Searches`, config hooks, community plugins. Where is "provider extension" documented?
- **For DeepSeek:** could `/v1/tools/search` and `/v1/tools/fetch` be exposed as a provider extension rather than core code? What would it take, and what can't a provider do today?

## Answer

Recorded in [provider MCP extension mechanisms](../research/provider-mcp-extension.md).

The short version:

- **MCP is one-way.** magpie serves `/mcp/magpie/web-search` (with `web_search` and `web_fetch`), `/mcp/magpie/vision`, and `/mcp/magpie/tools`. `/mcp/{name}` is not a provider's server: it is a proxy to a remote MCP server the user signed in to through the Library (`internal/mcpauth`).
- **No provider supplies MCP or tools to magpie.** Provider plugins expose `config` (including `searches` and `decide`), `auth`, `provider.models`, `chat.headers`, and `tool`. The `tool` hook's tools reach agents only at `/mcp/magpie/tools`; magpie's own `web_search`, `web_fetch`, and Settings searcher never consult a plugin.
- **Extension surfaces are:** built-in preset (core code), custom provider (`providers.json` key + URLs), OpenCode/pi provider plugin (Bun host), built-in `pluginrt` gateway points, and `magpie.hooks` gateway packages. "Provider extension" is not a repo term; the docs say "provider plugins".
- **DeepSeek needs core code.** `/v1/tools/search` and `/v1/tools/fetch` cannot be delivered by a provider plugin: a plugin has no searcher/fetch backend hook. They need a `provider.KimiCodeSearch`-style service searcher plus a new fetch backend seam behind `web_fetch`. A plugin could only add agent-callable `deepseek_search`/`deepseek_fetch` tools, which magpie's own search path would not use.

const fs = require("node:fs");

const root = ".scratch/upstream-sync-ad383351";
const log = fs.readFileSync(`${root}/ui-failed-files-serial-a47596ea.log`, "utf8");
const blocks = new Map();
for (const m of log.matchAll(/^test at (internal\/gui\/tests\/[^:\n]+):\d+:\d+\n([\s\S]*?)(?=^test at |\Z)/gm)) {
  const file = m[1];
  if (!blocks.has(file)) blocks.set(file, []);
  blocks.get(file).push(m[0]);
}
const groups = {
  settings: ["balance-fix", "cors-origins", "currency", "gateway-key-limit", "gateway-key-models", "lan-docker", "model-suffix-own", "number-units", "plain-names", "privacy-usage-stats", "provider-url-each-api", "s3-sync", "search-first", "tray-totals", "warm-at-own", "redact-rule-edit", "redact-words-any-case", "redact-words-whole"],
  routing: [],
  usage: ["recent-calls-more", "rest-try-now"],
  interaction: ["bare-ids", "click-scroll", "codex-predictions", "codex-titles", "fonts", "last-page", "menu-input-scroll", "phone-rows", "phone-web", "segs-press-redraw", "side-toggle", "title-bar-tools", "user-select", "zcode-site-layout"],
  plugins: ["library-mcp-tools", "omarchy", "plugin-card-update", "plugin-github", "plugin-move", "agent-update-all", "gateway-caller-keys", "plugin-github-filter", "view-wheel-unheard"],
};
for (const file of blocks.keys()) {
  const base = file.split("/").pop().replace(".test.cjs", "");
  if (base.startsWith("routing-")) groups.routing.push(base);
  else if (base.startsWith("usage-")) groups.usage.push(base);
}
const fixed = new Set(["i18n-keys", "old-webkit", "quota-refresh-order"]);
const assigned = new Set();
for (const [name, files] of Object.entries(groups)) {
  const unique = [...new Set(files)].sort();
  let text = "";
  for (const base of unique) {
    if (assigned.has(base)) throw new Error(`duplicate ${base}`);
    assigned.add(base);
    const file = `internal/gui/tests/${base}.test.cjs`;
    text += (blocks.get(file) || []).join("") + "\n";
  }
  fs.writeFileSync(`${root}/ui-failures-${name}.log`, text);
  console.log(`${name}: ${unique.length} files, ${(text.match(/^test at /gm) || []).length} failures`);
}
const missing = [...blocks.keys()].filter((file) => !fixed.has(file.split("/").pop().replace(".test.cjs", "")) && !assigned.has(file.split("/").pop().replace(".test.cjs", "")));
if (missing.length) throw new Error(`unassigned: ${missing.join(", ")}`);

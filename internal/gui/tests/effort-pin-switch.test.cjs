// Run with Node's test runner and Playwright on the module path; see README.md.
// The effort pin: every model an agent magpie has taken over starts at its
// own highest effort. Its switch is on the Agents page, above the agent
// list: it shows the setting as it is, says which rule is in force, and
// saves through the Settings page's own save, so nothing else changes. The
// Settings page was never opened. In English and Chinese, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang) {
  const settings = { lang, theme: "light" };
  const posts = [];
  const state = () => ({
    agents: [{
      id: "kimi-code", name: "Kimi Code", path: "/test/.kimi-code/config.toml", icon: "kimi", wired: true,
      fields: [{ key: "provider", label: "provider", value: "magpie", options: [{ value: "magpie", label: "magpie", icon: "magpie" }] }],
      models: { shown: 1, listed: 1, by: [{ name: "K3.1 Pro", icon: "kimi", n: 1 }], names: ["K3.1 Pro"] },
    }],
    profiles: [], settings,
  });
  async function serve(route) {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        posts.push(body);
        Object.assign(settings, body);
      }
      return json({ ...settings });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return { serve, posts, settings };
}

const W = {
  en: {
    say: "Every agent magpie has taken over starts its models at the highest effort",
    off: "Each model starts at its own default, the one its vendor gives it.",
    on: "Each model starts at its own highest effort, whatever was picked for it.",
  },
  zh: {
    say: "magpie 接管的每个代理，其模型都以最高思考力度启动",
    off: "每个模型都以自己的默认力度启动，即供应商给出的那一档。",
    on: "每个模型都以自己的最高思考力度启动，不管为它选了什么。",
  },
};
const line = "#agents > .ag-pin";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [1100, 640]) {
      test(`${engine} ${lang} ${width}px: the Agents page's switch pins every model's effort to the highest`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 700 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const fx = fixture(lang);
        await page.route("**/*", fx.serve);
        t.after(() => browser.close());
        await page.goto("http://magpie.test/");
        const sw = page.locator(`${line} .lib-switch`);
        await sw.waitFor();
        // the line is above the agent list, and says what the switch is for
        assert.equal(await page.locator(`${line} .ag-pin-say`).textContent(), w.say);
        const box = await page.locator(line).boundingBox();
        assert.ok(box.width <= width && box.height > 0, JSON.stringify(box));
        // the setting is off, and the hint says which rule is in force
        assert.equal(await sw.getAttribute("aria-checked"), "false", "on before it was switched");
        assert.equal(await page.locator(`${line} .ag-hint`).textContent(), w.off);
        const was = { ...fx.settings };

        const y = await page.evaluate(() => scrollY);
        await sw.click();
        await page.waitForFunction((sel) => document.querySelector(sel)?.getAttribute("aria-checked") === "true", `${line} .lib-switch`);
        // the save carries the pin, and nothing else changed
        assert.equal(fx.posts.length, 1);
        assert.equal(fx.posts[0].effortPin, "highest");
        for (const [k, v] of Object.entries(was)) assert.equal(fx.posts[0][k], v, `${k} changed`);
        // and the hint says the other rule now
        assert.equal(await page.locator(`${line} .ag-hint`).textContent(), w.on);

        // off again, by its words
        await page.locator(`${line} .ag-pin-say`).click();
        await page.waitForFunction((sel) => document.querySelector(sel)?.getAttribute("aria-checked") === "false", `${line} .lib-switch`);
        assert.equal(fx.posts.length, 2);
        assert.equal(fx.posts[1].effortPin, "");
        assert.equal(await page.locator(`${line} .ag-hint`).textContent(), w.off);

        assert.equal(await page.evaluate(() => scrollY), y, "a click scrolled the page");
        assert.equal(await page.locator("select").count(), 0, "a native select");
        // the Settings page was never opened
        assert.equal(await page.locator("#view-settings").isVisible(), false, "Settings was opened");
        assert.deepEqual(errors, []);
      });
    }
  }
}

const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium } = require("playwright");

(async () => {
  const assets = path.resolve("internal/gui/assets");
  const src = await fs.readFile(path.join(assets, "app.css"), "utf8");
  const m = src.match(/\.ic:is\(([^)]*)\) > \* \{ width: 82%; height: 82%; \}/);
  if (!m) throw new Error("tile rule not found");
  const listed = new Set([...m[1].matchAll(/\[data-icon="([^"]+)"\]/g)].map((x) => x[1]));
  const files = (await fs.readdir(path.join(assets, "icons"))).filter((f) => /\.(svg|png)$/.test(f));
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    await page.setContent("<html></html>");
    const found = new Set();
    for (const f of files) {
      const name = f.replace(/\.(svg|png)$/, "");
      const mime = f.endsWith(".png") ? "image/png" : "image/svg+xml";
      const url = `data:${mime};base64,` + (await fs.readFile(path.join(assets, "icons", f))).toString("base64");
      const r = await page.evaluate(async (url) => {
        const img = new Image();
        img.src = url;
        await img.decode();
        const S = 256, c = document.createElement("canvas");
        c.width = c.height = S;
        const x = c.getContext("2d");
        const nw = img.naturalWidth || S, nh = img.naturalHeight || S, k = Math.min(S / nw, S / nh);
        x.drawImage(img, (S - nw * k) / 2, (S - nh * k) / 2, nw * k, nh * k);
        const d = x.getImageData(0, 0, S, S).data;
        let l = S, t = S, rt = -1, b = -1, ink = 0;
        for (let y = 0; y < S; y++) for (let i = 0; i < S; i++) {
          if (d[(y * S + i) * 4 + 3] <= 24) continue;
          ink++; l = Math.min(l, i); rt = Math.max(rt, i); t = Math.min(t, y); b = Math.max(b, y);
        }
        return { w: (rt - l + 1) / S, h: (b - t + 1) / S, ink: ink / S / S };
      }, url);
      if (Math.min(r.w, r.h) >= 0.93 && r.ink >= 0.7) found.add(name);
    }
    console.log(JSON.stringify({ missing: [...found].filter((n) => !listed.has(n)), extra: [...listed].filter((n) => !found.has(n)), found: [...found] }, null, 2));
  } finally {
    await browser.close();
  }
})().catch((e) => { console.error(e); process.exitCode = 1; });

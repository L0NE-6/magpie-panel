// The Microsoft 365 settings row: opt in, show the loopback HTTPS endpoint,
// keep the key masked, and copy it only through the explicit route.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function payload(on = false, certificateActions = false) {
  return {
    theme: "light",
    lang: "en",
    tray: "panel",
    currency: "usd",
    version: "0.1.4786",
    dir: "/config/magpie",
    gateway: "http://127.0.0.1:3425",
    login: false,
    lan: false,
    lanURLs: [],
    corsOrigins: on ? ["https://pivot.claude.ai"] : [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    m365: on,
    m365KeyName: on ? "Claude for Microsoft 365" : "",
    m365KeyMasked: on ? "sk-magpie…abcdef" : "",
    m365CertificateActions: certificateActions,
    m365Status: {
      running: on,
      url: "https://127.0.0.1:8787",
      certificate: {
        ready: on,
        trusted: false,
        certificate: "/config/magpie/m365/local-ca.pem",
        message: "Trust the local certificate before connecting from Microsoft 365",
      },
    },
  };
}

function server(calls) {
  let current = payload();
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data, status = 200) => route.fulfill({ json: data, status });
    if (url.pathname === "/boot.js") {
      return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"en",theme:"light"};` });
    }
    if (url.pathname === "/wails/runtime.js") {
      return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    }
    if (url.pathname === "/api/state") {
      return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" }, fx: current.fx });
    }
    if (url.pathname === "/api/settings") return json(current);
    if (url.pathname === "/api/settings/m365") {
      calls.push("toggle:" + route.request().postDataJSON().on);
      current = payload(route.request().postDataJSON().on);
      return json({ settings: current });
    }
    if (url.pathname === "/api/settings/m365/key") {
      calls.push("key");
      return json({ secret: "sk-magpie-key-fixture" });
    }
    if (url.pathname === "/api/copy") {
      calls.push("copy:" + route.request().postDataJSON().text);
      return json({});
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const ext = path.extname(file);
    return route.fulfill({
      body: await fs.readFile(file),
      contentType: {
        ".html": "text/html",
        ".js": "text/javascript",
        ".css": "text/css",
        ".svg": "image/svg+xml",
        ".png": "image/png",
      }[ext],
    });
  };
}

test("Microsoft 365 HTTPS setup stays explicit and masks the key", async (t) => {
  const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : { channel: "chromium" });
  t.after(() => browser.close());
  const calls = [];
  const context = await browser.newContext({ viewport: { width: 440, height: 760 }, reducedMotion: "reduce" });
  const page = await context.newPage();
  await page.route("**/*", server(calls));
  await page.goto("http://127.0.0.1:4999/?view=settings&tab=network");
  const box = page.locator("#m365List");
  await box.getByText("Claude for Microsoft 365", { exact: true }).waitFor();
  await box.getByRole("button", { name: "On", exact: true }).click();
  await box.getByText("https://127.0.0.1:8787", { exact: true }).waitFor();
  await box.getByText("/config/magpie/m365/local-ca.pem", { exact: true }).waitFor();
  assert.equal(await box.locator(".m365-key").textContent(), "sk-magpie…abcdef");
  await box.getByRole("button", { name: "Copy key", exact: true }).click();
  await page.waitForFunction(() => document.querySelector("#status")?.textContent);
  assert(calls.includes("toggle:true"));
  assert(calls.includes("key"));
  assert(calls.includes("copy:sk-magpie-key-fixture"));
  assert.equal(await page.locator("html").evaluate((e) => e.scrollWidth <= e.clientWidth), true);
  await context.close();
});

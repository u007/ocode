// Reproduce "chrome cdp on browser tab loads forever, nothing" in the real
// SPA. Opens the side browser panel, navigates to a URL, and reports the
// panel state: address bar, loading indicators, canvas frames, console.
import { chromium } from "playwright";

const MAIN = "http://127.0.0.1:4096";
const TARGET = process.argv[2] || "https://example.com";
const WAIT_MS = Number(process.argv[3] || 12000);

const log = (...a) => console.log(...a);

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
page.setDefaultTimeout(8000);

const consoleErrors = [];
page.on("console", (m) => {
  if (m.type() === "error") consoleErrors.push(m.text());
});
page.on("pageerror", (e) => consoleErrors.push("PAGEERROR: " + e.message));

await page.goto(MAIN, { waitUntil: "domcontentloaded" });
await page.waitForSelector("[aria-label='Toggle browser panel']", { timeout: 15000 });
log("app loaded; toggling browser panel");
await page.click("[aria-label='Toggle browser panel']");

await page.waitForSelector("[aria-label='Address']", { timeout: 10000 });
log("panel open; typing", TARGET);
await page.fill("[aria-label='Address']", TARGET);
await page.press("[aria-label='Address']", "Enter");
log("navigating; waiting", WAIT_MS, "ms");
await page.waitForTimeout(WAIT_MS);

// Collect state.
const state = await page.evaluate(() => {
  const q = (sel) => document.querySelector(sel);
  const spinner = q("[data-testid='cdp-spinner']");
  const reconnect = q("[data-testid='cdp-reconnecting']");
  const errText = q("[data-testid='browser-panel'] [class*='text-red']")?.textContent;
  const loadingBar = q("[data-testid='browser-loading-bar']");
  const canvas = q("canvas");
  const address = q("[aria-label='Address']")?.value;
  const status = q("[data-testid^='browser-']")?.getAttribute("data-testid");
  const banner = q("[data-testid='chrome-mode-banner']")?.textContent;
  const urlBar = document.querySelector("[aria-label='Address']")?.value;
  return {
    address,
    urlBar,
    hasCanvas: !!canvas,
    canvasSize: canvas ? `${canvas.width}x${canvas.height}` : null,
    cdpSpinnerVisible: !!spinner,
    reconnectingVisible: !!reconnect,
    loadingBarVisible: !!loadingBar,
    errorText: errText ?? null,
    banner: banner ?? null,
    panelTestId: status,
  };
});

await page.screenshot({ path: "/tmp/cdp-repro-panel.png" });
log("STATE:", JSON.stringify(state, null, 2));
log("CONSOLE ERRORS:", consoleErrors.length ? consoleErrors.join("\n") : "(none)");
await browser.close();
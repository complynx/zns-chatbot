"use strict";

// Use a new browser context for each synthetic user. The caller owns its browser.
// Only the mock stand's HTTPS origin is reachable; the Telegram SDK is a fixture.
async function openMiniApp(browser, { origin, initData, locale = "en" }) {
  const url = new URL(origin);
  if (url.protocol !== "https:" || url.hostname !== "localhost" || (!url.port || Number(url.port) < 20000 || Number(url.port) > 60005) || url.pathname !== "/" || url.username || url.password || url.search || url.hash) {
    throw new Error("Expected an isolated mock stand HTTPS origin on localhost:20000..60005");
  }
  if (typeof initData !== "string" || !initData || initData.length > 16384) throw new Error("Expected synthetic signed initData");
  // TLS verification is relaxed only in this disposable fixture context. This
  // does not install a CA or alter the user's browser or production configuration.
  const context = await browser.newContext({ ignoreHTTPSErrors: true, locale, serviceWorkers: "block" });
  context.setDefaultTimeout(15000);
  try {
    await context.route("**/*", async route => {
      const request = new URL(route.request().url());
      if (request.href === "https://telegram.org/js/telegram-web-app.js?63") {
        return route.fulfill({ contentType: "application/javascript", body: `window.Telegram={WebApp:{initData:${JSON.stringify(initData)},ready(){}}};` });
      }
      if (request.origin === url.origin) return route.continue();
      return route.abort("blockedbyclient");
    });
    return { context, page: await context.newPage() };
  } catch (error) {
    await context.close();
    throw error;
  }
}

module.exports = { openMiniApp };

// Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
// on behalf of Daniel Drizhuk

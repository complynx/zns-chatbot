import { chromium } from 'playwright';
import { userID } from './client.mjs';

export async function openTelegram(
  run,
  {
    touch = false,
    subject = 'alice',
    channel = process.env.BROWSER_CHANNEL,
    headless = true,
  } = {},
) {
  const browser = await chromium.launch({
    headless,
    ...(channel && { channel }),
  });
  try {
    const context = await browser.newContext({
      viewport: touch
        ? { width: 390, height: 844 }
        : { width: 1440, height: 1000 },
      hasTouch: touch,
      isMobile: touch,
    });
    await context.tracing.start({ screenshots: true, snapshots: true });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => {
      errors.push(String(error));
    });
    await page.goto(run.client.ui);
    await page.locator('#user').selectOption(String(userID(subject)));
    const click = (locator) => (touch ? locator.tap() : locator.click());
    const driver = {
      page,
      click,
      // Observe the real browser input response; completion tracks update ID, not message count.
      async act(action) {
        const [response] = await Promise.all([
          page.waitForResponse(
            (r) =>
              r.url() === run.client.ui + '/lab/input' &&
              r.request().method() === 'POST',
          ),
          action(),
        ]);
        if (!response.ok())
          throw new Error(`Telegram input rejected: ${response.status()}`);
        const update = await response.json();
        await run.client.processed(subject, update.update_id);
        return update;
      },
      async send(text) {
        await page.locator('#text').fill(text);
        return driver.act(() => click(page.locator('#chat button')));
      },
      card(messageID) {
        return page.locator(`.message[data-message-id="${Number(messageID)}"]`);
      },
      async download(link, name) {
        const [download] = await Promise.all([
          page.waitForEvent('download'),
          click(link),
        ]);
        const failure = await download.failure();
        if (failure) throw new Error(failure);
        await download.saveAs(run.artifact(name));
        return {
          filename: download.suggestedFilename(),
          path: run.artifact(name),
        };
      },
      async close(label = touch ? 'touch' : 'mouse') {
        try {
          await page.screenshot({
            path: run.artifact(label + '.png'),
            fullPage: true,
          });
          await run.save(label + '-browser-errors.json', errors);
          await context.tracing.stop({
            path: run.artifact(label + '-trace.zip'),
          });
        } finally {
          await browser.close();
        }
      },
    };
    return driver;
  } catch (error) {
    await browser.close();
    throw error;
  }
}

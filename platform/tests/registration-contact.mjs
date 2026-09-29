import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const base = process.env.SANDBOX_URL;
if (!base || !['127.0.0.1', 'localhost'].includes(new URL(base).hostname))
  throw new Error('SANDBOX_URL must be a local fake Telegram server');
const browser = await chromium.launch({
  headless: true,
  ...(process.env.BROWSER_CHANNEL && { channel: process.env.BROWSER_CHANNEL }),
});
await fs.mkdir('test-results/registration-contact', { recursive: true });
try {
  for (const touch of [false, true]) {
    const context = await browser.newContext({
      viewport: touch
        ? { width: 390, height: 844 }
        : { width: 1300, height: 950 },
      hasTouch: touch,
      isMobile: touch,
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => {
      errors.push(String(error));
      console.error(String(error));
    });
    await page.goto(base);
    await page.locator('#contact-user').selectOption('202');
    for (const mode of ['contact', 'forward', 'hidden']) {
      await page.locator('#contact-mode').selectOption(mode);
      await page
        .locator('#contact-text')
        .fill(mode + (touch ? ' touch' : ' mouse'));
      const response = page.waitForResponse(
        (value) =>
          value.url().endsWith('/lab/input') &&
          value.request().method() === 'POST',
      );
      const button = page.locator('#contact-form button');
      const [wire] = await Promise.all([
        response,
        touch ? button.tap() : button.click(),
      ]);
      assert.equal(wire.status(), 200);
      const update = await wire.json();
      if (mode === 'contact') assert.equal(update.message.contact.user_id, 202);
      else if (mode === 'forward') {
        assert.equal(update.message.forward_origin.type, 'user');
        assert.equal(update.message.forward_origin.sender_user.id, 202);
      } else {
        assert.equal(update.message.forward_origin.type, 'hidden_user');
        assert.equal(update.message.forward_origin.sender_user, undefined);
      }
      const card = page.locator(
        `.message[data-message-id="${update.message.message_id}"]`,
      );
      await card.waitFor();
      assert.match(
        await card.textContent(),
        mode === 'hidden' ? /Hidden sender/ : /202/,
      );
    }
    await page.screenshot({
      path: `test-results/registration-contact/${touch ? 'touch' : 'mouse'}.png`,
      fullPage: true,
    });
    assert.deepEqual(errors, []);
    await context.close();
  }
  console.log(
    'Contact, visible forward and hidden sender: mouse and touch passed.',
  );
} finally {
  await browser.close();
}

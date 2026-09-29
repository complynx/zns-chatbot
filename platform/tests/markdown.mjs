// Runs against an isolated fake Telegram test server. It never contacts Telegram.
import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const base = process.env.SANDBOX_URL;
if (!base || !['127.0.0.1', 'localhost'].includes(new URL(base).hostname))
  throw new Error('SANDBOX_URL must identify a local fake Telegram server');
const browser = await chromium.launch({
  headless: true,
  ...(process.env.BROWSER_CHANNEL && { channel: process.env.BROWSER_CHANNEL }),
});
const post = async (method, payload) => {
  const response = await fetch(base + '/botsandbox/' + method, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  assert.equal(response.status, 200);
  const result = await response.json();
  assert.equal(result.ok, true);
  return result.result;
};
await fs.mkdir('test-results/markdown', { recursive: true });
try {
  for (const touch of [false, true]) {
    const context = await browser.newContext({
      viewport: touch
        ? { width: 390, height: 844 }
        : { width: 1400, height: 1000 },
      hasTouch: touch,
      isMobile: touch,
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => {
      errors.push(String(error));
    });
    const sent = await post('sendMessage', {
      chat_id: 101,
      parse_mode: 'MarkdownV2',
      text: '*Bold 🚀* _italic_ [Даня](tg://user?id=101) [chat](https://t.me/public_chat) [topic](https://t.me/c/123/7/9?thread=7&single)\n\n>quote\n>second\n\n`a_b`\n\n```go\nx := 1\n```',
    });
    assert.ok(
      sent.entities.some(
        (entity) => entity.type === 'text_mention' && entity.user.id === 101,
      ),
    );
    await page.goto(base);
    const card = page.locator(`.message[data-message-id="${sent.message_id}"]`);
    await card.locator('strong').waitFor();
    assert.equal(await card.locator('strong').textContent(), 'Bold 🚀');
    assert.equal(await card.locator('em').textContent(), 'italic');
    assert.equal(await card.locator('code').textContent(), 'a_b');
    assert.equal(await card.locator('pre').textContent(), 'x := 1\n');
    assert.equal(
      await card.locator('blockquote').textContent(),
      'quote\nsecond',
    );
    // Observe the native anchor target while suppressing external navigation in QA.
    await page.evaluate(() => {
      document.addEventListener(
        'click',
        (event) => {
          const link = event.target.closest('a');
          if (!link) return;
          event.preventDefault();
          document.documentElement.dataset.clickedLink =
            link.getAttribute('href');
        },
        { capture: true },
      );
    });
    for (const [label, target] of [
      ['Даня', 'tg://user?id=101'],
      ['chat', 'https://t.me/public_chat'],
      ['topic', 'https://t.me/c/123/7/9?thread=7&single'],
    ]) {
      const link = card.getByRole('link', { name: label, exact: true });
      assert.equal(await link.getAttribute('href'), target);
      if (touch) await link.tap();
      else {
        await link.hover();
        await link.click();
      }
      assert.equal(
        await page.locator('html').getAttribute('data-clicked-link'),
        target,
      );
    }
    await page.screenshot({
      path: `test-results/markdown/${touch ? 'touch' : 'mouse'}-formatted.png`,
      fullPage: true,
    });
    await post('editMessageText', {
      chat_id: 101,
      message_id: sent.message_id,
      parse_mode: 'MarkdownV2',
      text: '*Edited 🚀* [other](https://t.me/public_chat/321)',
    });
    await page.waitForFunction(
      (id) =>
        document.querySelector(`[data-message-id="${CSS.escape(id)}"] strong`)
          ?.textContent === 'Edited 🚀',
      sent.message_id,
    );
    assert.equal(await card.getByRole('link').count(), 1);
    assert.equal(
      await card.getByRole('link').getAttribute('href'),
      'https://t.me/public_chat/321',
    );
    const literal =
      'Name A_*B! [literal](https://t.me/name) <img src=x onerror=alert(1)>';
    await post('editMessageText', {
      chat_id: 101,
      message_id: sent.message_id,
      text: literal,
    });
    await page.waitForFunction(
      ({ id, literal }) =>
        document.querySelector(`[data-message-id="${CSS.escape(id)}"] p`)
          ?.textContent === literal,
      { id: sent.message_id, literal },
    );
    assert.equal(
      await card.locator('a,strong,img,code,pre,blockquote').count(),
      0,
    );
    assert.deepEqual(errors, []);
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
      'markdown UI must fit the viewport',
    );
    await page.screenshot({
      path: `test-results/markdown/${touch ? 'touch' : 'mouse'}-literal.png`,
      fullPage: true,
    });
    await context.close();
  }
  console.log(
    'Markdown GUI passed: mouse/touch links, formats, UTF-16 text, edited state, literal fallback DOM.',
  );
} finally {
  await browser.close();
}

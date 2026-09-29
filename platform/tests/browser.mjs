// Runs against the isolated Compose sandbox, never against Telegram itself.
import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
const base = process.env.SANDBOX_URL || 'http://127.0.0.1:8090';
const browser = await chromium.launch({
  headless: true,
  ...(process.env.BROWSER_CHANNEL && { channel: process.env.BROWSER_CHANNEL }),
});
await fs.mkdir('test-results', { recursive: true });
try {
  for (const touch of [false, true]) {
    const context = await browser.newContext({
      viewport: touch
        ? { width: 390, height: 844 }
        : { width: 1440, height: 1000 },
      hasTouch: touch,
      isMobile: touch,
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => {
      errors.push(String(error));
    });
    const click = async (locator) => (touch ? locator.tap() : locator.click());
    const card = page
      .locator('.message[data-bot="true"]')
      .filter({ hasText: /Статус: (empty|draft|booked|cancelled)/ })
      .last();
    const waitText = async (text) => {
      await page.waitForFunction(
        (text) =>
          [...document.querySelectorAll('.message[data-bot="true"]')].some(
            (message) => message.textContent.includes(text),
          ),
        text,
      );
    };
    await page.goto(base);
    await click(page.locator('#start'));
    await card.waitFor();
    if (await card.getByRole('button', { name: 'Отменить заявку' }).count()) {
      await click(card.getByRole('button', { name: 'Отменить заявку' }));
      await waitText('Статус: cancelled');
    }
    await click(card.getByRole('button', { name: /Трансфер/ }));
    await waitText('Статус: draft');
    await page.locator('#text').fill('помоги закончить');
    await click(page.getByRole('button', { name: 'Отправить', exact: true }));
    await waitText('Черновик сохранён');
    const before = await page.locator('.message[data-bot="true"]').count();
    await click(card.getByRole('button', { name: 'Подтвердить', exact: true }));
    await waitText('Статус: booked');
    assert.equal(
      await page.locator('.message[data-bot="true"]').count(),
      before,
      'shared card should edit in place',
    );
    await click(page.getByRole('button', { name: 'Следующий запрос: 429' }));
    await click(card.getByRole('button', { name: 'Отменить заявку' }));
    await waitText('Статус: cancelled');
    await page.locator('#user').selectOption('303');
    await click(page.locator('#start'));
    await waitText('Статус: empty');
    await click(card.getByRole('button', { name: /Массаж/ }));
    await waitText('forbidden');
    await page.locator('#text').fill('выбери массаж');
    await click(page.getByRole('button', { name: 'Отправить', exact: true }));
    await page.waitForFunction(() =>
      document
        .querySelector('#history')
        .textContent.includes('"origin": "agent"'),
    );
    await waitText('forbidden');
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
      'page overflows',
    );
    assert.deepEqual(errors, []);
    await page.screenshot({
      path: `test-results/${touch ? 'touch' : 'desktop'}.png`,
      fullPage: true,
    });
    await context.close();
  }
  console.log(
    'Browser checks passed: mouse, emulated touch, mixed workflow, 429 retry, denied manual/agent access.',
  );
} finally {
  await browser.close();
}

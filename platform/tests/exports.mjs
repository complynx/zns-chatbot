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
    const click = (locator) => (touch ? locator.tap() : locator.click());
    const send = async (text) => {
      await page.locator('#text').fill(text);
      await click(page.locator('#chat button'));
    };
    await page.goto(base);
    await page.locator('#user').selectOption('202');
    await click(page.locator('#orders'));
    const links = page
      .locator('.message[data-bot="true"] a')
      .filter({ hasText: 'Документ: orders.xlsx' });
    const downloadNext = async (action) => {
      const before = await links.count();
      await action();
      const link = links.nth(before);
      await link.waitFor();
      const [download] = await Promise.all([
        page.waitForEvent('download'),
        click(link),
      ]);
      assert.equal(download.suggestedFilename(), 'orders.xlsx');
      const body = Buffer.concat(
        await Array.fromAsync(await download.createReadStream()),
      );
      assert.equal(body.subarray(0, 4).toString('hex'), '504b0304');
      assert.ok(body.length > 2000);
      await download.saveAs(
        `test-results/export-${touch ? 'touch' : 'mouse'}.xlsx`,
      );
    };
    await downloadNext(() =>
      click(page.getByRole('button', { name: '📥 XLSX', exact: true })),
    );
    await downloadNext(() => send('/exportfoodorders'));
    await downloadNext(() => send('Экспорт заказов в xlsx'));
    await links.last().scrollIntoViewIfNeeded();
    await page.screenshot({
      path: `test-results/exports-${touch ? 'touch' : 'mouse'}.png`,
    });
    for (const user of ['101', '303']) {
      await page.locator('#user').selectOption(user);
      await click(page.locator('#orders'));
      await page
        .locator('.message')
        .filter({ hasText: 'Заказы фестиваля' })
        .waitFor();
      assert.equal(
        await page
          .getByRole('button', { name: '📥 XLSX', exact: true })
          .count(),
        0,
      );
      await send('/exportfoodorders');
      await page
        .locator('.message')
        .filter({ hasText: 'Экспорт недоступен: forbidden' })
        .waitFor();
      const request = `Экспорт заказов в xlsx ${Date.now()}`;
      const [accepted] = await Promise.all([
        page.waitForResponse(
          (response) =>
            new URL(response.url()).pathname === '/lab/input' &&
            response.request().method() === 'POST',
        ),
        send(request),
      ]);
      assert.equal(accepted.status(), 200);
      const update = await accepted.json();
      await page.waitForFunction((updateID) => {
        const history = JSON.parse(
          document.querySelector('#history').textContent,
        );
        return history.some(
          (event) =>
            event.kind === 'orders_reply' && event.update_id === updateID,
        );
      }, update.update_id);
      assert.equal(await links.count(), 0);
    }
    assert.deepEqual(errors, []);
    await context.close();
  }
} finally {
  await browser.close();
}
console.log(
  'Export browser checks passed: mouse/touch, button/command/agent downloads, denied manual and agent requests.',
);

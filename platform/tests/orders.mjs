import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const base = process.env.SANDBOX_URL || 'http://127.0.0.1:8090';
const browser = await chromium.launch({
  headless: true,
  ...(process.env.BROWSER_CHANNEL && { channel: process.env.BROWSER_CHANNEL }),
});
await fs.mkdir('test-results', { recursive: true });
async function waitText(locator, text) {
  await locator.filter({ hasText: text }).waitFor();
}
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
    await page.goto(base);
    await click(page.locator('#orders'));
    await click(page.getByRole('button', { name: 'Новый заказ', exact: true }));
    const newest = page
      .locator('.message[data-bot="true"]')
      .filter({ hasText: /Заказ \w+\nСтатус: Не оплачен/ })
      .last();
    await newest.waitFor();
    const messageId = await newest.getAttribute('data-message-id');
    const card = page.locator(`[data-message-id="${messageId}"]`);
    const initialText = await card.textContent();
    const orderId = initialText.match(/Заказ (\w+)/)[1];
    await click(
      card.getByRole('button', { name: 'Добавить: Трансфер', exact: true }),
    );
    await waitText(card, 'Итого: 65,00 BYN');
    await page.locator('#text').fill('добавь препати в заказ');
    await click(page.getByRole('button', { name: 'Отправить', exact: true }));
    await waitText(card, 'Итого: 100,00 BYN');
    assert.equal(await card.getAttribute('data-message-id'), messageId);
    assert.ok(
      await page
        .locator('.message[data-bot="false"]')
        .filter({ hasText: 'добавь препати' })
        .count(),
    );
    await click(
      card.getByRole('button', { name: 'Наличные: Борис', exact: true }),
    );
    await waitText(card, 'Статус: Ожидается подтверждение наличных');
    await page.locator('#user').selectOption('202');
    await click(page.locator('#orders'));
    const payment = page
      .locator('.message[data-bot="true"]')
      .filter({ hasText: 'Проверка оплаты' })
      .filter({ hasText: orderId });
    await click(
      payment.getByRole('button', { name: 'Принять оплату', exact: true }),
    );
    await payment.waitFor({ state: 'detached' });
    await page.locator('#user').selectOption('101');
    await waitText(card, 'Статус: Оплачен');
    assert.equal(await card.getByRole('button').count(), 1);
    assert.equal(
      await card.getByRole('button', { name: 'Питание и ФИО' }).count(),
      1,
    );
    const paidText = await card.textContent();
    assert.ok(paidText.includes('Трансфер: 65,00 BYN'));
    await card.scrollIntoViewIfNeeded();
    await page.screenshot({
      path: `test-results/orders-${touch ? 'touch' : 'mouse'}.png`,
    });
    await page.locator('#user').selectOption('303');
    await click(page.locator('#orders'));
    await click(page.getByRole('button', { name: 'Новый заказ', exact: true }));
    await page
      .locator('.message[data-bot="true"]')
      .filter({ hasText: 'Действие отклонено: forbidden' })
      .waitFor();
    assert.deepEqual(errors, []);
    await context.close();
  }
  console.log(
    'Orders browser checks passed: mouse/touch, manual+agent edits, cash approval, in-place updates, denied access.',
  );
} finally {
  await browser.close();
}

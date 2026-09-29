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
    const click = (locator) => (touch ? locator.tap() : locator.click());
    await page.goto(base);
    await click(page.locator('#orders'));
    const frame = page.frameLocator('#miniapp-frame');
    const paid = page
      .locator('.message[data-bot="true"]')
      .filter({ hasText: 'Статус: Оплачен' })
      .first();
    await click(
      paid.getByRole('button', { name: 'Питание и ФИО', exact: true }),
    );
    await waitText(frame.locator('#status'), 'только для просмотра');
    assert.equal(await frame.locator('#save').isDisabled(), true);
    assert.equal(
      await frame.locator('[name="customer_first_name"]').isDisabled(),
      true,
    );
    await click(page.locator('#miniapp-close'));
    await click(page.getByRole('button', { name: 'Новый заказ', exact: true }));
    const newest = page
      .locator('.message[data-bot="true"]')
      .filter({ hasText: /Заказ \w+\nСтатус: Не оплачен/ })
      .last();
    await newest.waitFor();
    const messageId = await newest.getAttribute('data-message-id');
    const card = page.locator(`[data-message-id="${messageId}"]`);
    await click(
      card.getByRole('button', { name: 'Питание и ФИО', exact: true }),
    );
    await frame.locator('[name="customer_first_name"]').fill('Alice');
    await frame.locator('[name="customer_last_name"]').fill('Example');
    const friday = frame
      .locator('details')
      .filter({ hasText: 'Пятница · Ужин' });
    await click(friday.locator('summary'));
    await friday
      .getByRole('spinbutton', { name: 'Цезарь', exact: true })
      .fill('2');
    await click(frame.locator('#quote'));
    await waitText(frame.locator('#summary'), '23.30 BYN');
    await waitText(frame.locator('#summary'), 'Контейнер для салата × 2');
    await click(frame.locator('#save'));
    await waitText(frame.locator('#status'), 'Заказ сохранён');
    await click(page.locator('#miniapp-close'));
    await waitText(card, '23.30 BYN');
    await waitText(card, 'Alice Example');
    await page.locator('#text').fill('добавь препати в заказ');
    await click(page.getByRole('button', { name: 'Отправить', exact: true }));
    await waitText(card, '58.30 BYN');
    await click(
      card.getByRole('button', { name: 'Питание и ФИО', exact: true }),
    );
    await frame.locator('#language').selectOption('en');
    const dinner = frame
      .locator('details')
      .filter({ hasText: 'Friday · Dinner' });
    await click(dinner.locator('summary'));
    const quantity = dinner.getByRole('spinbutton', {
      name: 'Caesar Salad',
      exact: true,
    });
    assert.equal(await quantity.inputValue(), '2');
    assert.equal(
      await frame.locator('[name="customer_first_name"]').inputValue(),
      'Alice',
    );
    await quantity.fill('3');
    await click(frame.locator('#quote'));
    await waitText(frame.locator('#summary'), '69.80 BYN');

    const second = await context.newPage();
    await second.goto(base);
    const secondCard = second.locator(`[data-message-id="${messageId}"]`);
    await click(
      secondCard.getByRole('button', {
        name: 'Добавить: Трансфер',
        exact: true,
      }),
    );
    await waitText(secondCard, '123.30 BYN');
    await click(frame.locator('#save'));
    await waitText(frame.locator('#status'), 'Your draft remains');
    assert.equal(await quantity.inputValue(), '3');
    await click(frame.locator('#reload'));
    await waitText(frame.locator('#summary'), '123.30 BYN');
    await click(dinner.locator('summary'));
    assert.equal(await quantity.inputValue(), '2');
    await quantity.fill('3');
    await click(frame.locator('#quote'));
    await waitText(frame.locator('#summary'), '134.80 BYN');
    let hasDropped = false;
    let committedVersion;
    await page.route('**/miniapp/api/orders/*', async (route) => {
      if (!hasDropped && route.request().method() === 'POST') {
        hasDropped = true;
        const response = await route.fetch();
        const saved = await response.json();
        assert.equal(response.status(), 200);
        committedVersion = saved.version;
        await route.abort('failed');
      } else await route.continue();
    });
    await click(frame.locator('#save'));
    await waitText(frame.locator('#status'), 'Retry Save');
    await click(frame.locator('#save'));
    await waitText(frame.locator('#status'), 'Order saved');
    await page.unroute('**/miniapp/api/orders/*');
    await page.screenshot({
      path: `test-results/meals-${touch ? 'touch' : 'mouse'}.png`,
    });
    await click(page.locator('#miniapp-close'));
    await waitText(card, '134.80 BYN');
    await waitText(card, 'версия ' + committedVersion);
    await click(
      card.getByRole('button', { name: 'Удалить заказ', exact: true }),
    );
    await waitText(card, 'Заказ удалён');
    assert.deepEqual(errors, []);
    await second.close();
    await context.close();
  }
  console.log(
    'Meal editor passed: mouse/touch, signed launch, quote, save, agent continuation, stale draft, exact retry.',
  );
} finally {
  await browser.close();
}

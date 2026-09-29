import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs/promises';

const base = process.env.SANDBOX_URL || 'http://127.0.0.1:8090';
function fixture(values) {
  const context =
    process.platform === 'win32' ? ['--context', 'desktop-linux'] : [];
  const environment = Object.entries(values).flatMap(([key, value]) => [
    '-e',
    `${key}=${value}`,
  ]);
  execFileSync(
    'docker',
    [
      ...context,
      'compose',
      'run',
      '--rm',
      '--no-deps',
      ...environment,
      'migrate',
      'fixture',
    ],
    { stdio: 'pipe' },
  );
}
const waitText = (locator, text) => locator.filter({ hasText: text }).waitFor();
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
    try {
      await page.goto(base);
      await click(page.locator('#orders'));
      const previousCards = await page
        .locator('.message[data-bot="true"]')
        .evaluateAll((cards) => cards.map((card) => card.dataset.messageId));
      await click(
        page.getByRole('button', { name: 'Новый заказ', exact: true }),
      );
      await page.waitForFunction(
        (previous) =>
          [...document.querySelectorAll('.message[data-bot="true"]')].some(
            (card) =>
              !previous.includes(card.dataset.messageId) &&
              /Заказ \w+\nСтатус: Не оплачен/.test(card.textContent),
          ),
        previousCards,
      );
      const newest = page
        .locator('.message[data-bot="true"]')
        .filter({ hasText: /Заказ \w+\nСтатус: Не оплачен/ })
        .last();
      await newest.waitFor();
      const messageId = await newest.getAttribute('data-message-id');
      const card = page.locator(`[data-message-id="${messageId}"]`);
      const text = await card.textContent();
      const id = text.match(/Заказ (\w+)/)[1];
      console.log(`Notification fixture ${id}`);
      const notices = page
        .locator('.message[data-bot="true"]')
        .filter({ hasText: `Заказ ${id}.` });
      await click(
        card.getByRole('button', { name: 'Добавить: Препати', exact: true }),
      );
      await waitText(card, '35,00 BYN');
      await click(
        card.getByRole('button', { name: 'Наличные: Борис', exact: true }),
      );
      await waitText(card, 'Статус: Ожидается подтверждение наличных');
      await page.locator('#user').selectOption('202');
      await waitText(notices, 'Ожидает проверки оплаты');
      const inbox = page
        .locator('.message[data-bot="true"]')
        .filter({ hasText: 'Проверка оплаты' })
        .filter({ hasText: id });
      await click(
        inbox.getByRole('button', { name: 'Отклонить оплату', exact: true }),
      );
      await page.locator('#user').selectOption('101');
      await waitText(notices, 'Оплата отклонена');
      await waitText(card, 'Статус: Не оплачен');
      fixture({ ORDER_FIXTURE_ID: id, ORDER_FIXTURE_AGE: '72h' });
      await waitText(notices, 'Напоминание об оплате');
      await notices
        .filter({ hasText: 'Напоминание об оплате' })
        .scrollIntoViewIfNeeded();
      await page.screenshot({
        path: `test-results/notifications-${touch ? 'touch' : 'mouse'}.png`,
      });
      fixture({ ORDER_FIXTURE_ADMIN_COUNTRY: 'ru' });
      await click(
        card.getByRole('button', { name: 'Отправить чек', exact: true }),
      );
      await page
        .locator('#messages')
        .getByText('Пришлите чек документом', { exact: false })
        .waitFor();
      await page.locator('#document').setInputFiles({
        name: 'ru-payment.pdf',
        mimeType: 'application/pdf',
        buffer: Buffer.from('%PDF-1.4 RU receipt'),
      });
      await click(page.locator('#send-document'));
      const pending = page
        .locator('.message[data-bot="true"]')
        .filter({
          has: page.getByRole('button', {
            name: /^(Это чек|This is a receipt)$/,
          }),
        })
        .last();
      await pending.waitFor();
      const mediaId = await pending.getAttribute('data-message-id');
      const mediaCard = page.locator(`[data-message-id="${mediaId}"]`);
      await click(
        mediaCard.getByRole('button', {
          name: /^(Это чек|This is a receipt)$/,
        }),
      );
      await click(mediaCard.getByRole('button').filter({ hasText: id }));
      await waitText(card, 'Статус: Чек на проверке');
      fixture({ ORDER_FIXTURE_DEADLINE: '2020-01-01T00:00:00Z' });
      await click(
        card.getByRole('button', { name: 'Питание и ФИО', exact: true }),
      );
      const editor = page.frameLocator('#miniapp-frame');
      await waitText(editor.locator('#status'), 'только для просмотра');
      assert.equal(await editor.locator('#save').isDisabled(), true);
      await click(page.locator('#miniapp-close'));
      await click(
        card.getByRole('button', { name: 'Оплата RU: Борис', exact: true }),
      );
      await waitText(card, '1\u{A0}050,00 RUB');
      await page.locator('#user').selectOption('202');
      await waitText(inbox, '1\u{A0}050,00 RUB');
      await click(
        inbox.getByRole('button', { name: 'Принять оплату', exact: true }),
      );
      await page.locator('#user').selectOption('101');
      await waitText(notices, 'Оплата подтверждена');
      await waitText(card, 'Статус: Оплачен');
      assert.deepEqual(errors, []);
    } finally {
      fixture({
        ORDER_FIXTURE_DEADLINE: '2030-09-24T21:00:00Z',
        ORDER_FIXTURE_ADMIN_COUNTRY: 'be',
      });
      await context.close();
    }
  }
  console.log(
    'Notification browser checks passed: proactive admin/customer, overdue reminder, RU routing after cutoff, mouse/touch.',
  );
} finally {
  await browser.close();
}

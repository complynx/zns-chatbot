import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const base = process.env.SANDBOX_URL || 'http://127.0.0.1:8090';
const browser = await chromium.launch({
  headless: true,
  ...(process.env.BROWSER_CHANNEL && { channel: process.env.BROWSER_CHANNEL }),
});
await fs.mkdir('test-results', { recursive: true });
const receipt = Buffer.from('%PDF-1.4\nSandbox payment receipt\n');
const waitText = (locator, text) => locator.filter({ hasText: text }).waitFor();
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
    const upload = async (orderId) => {
      await page.locator('#upload-kind').selectOption('document');
      await page.locator('#caption').fill('');
      await page.locator('#document').setInputFiles({
        name: 'receipt.pdf',
        mimeType: 'application/pdf',
        buffer: receipt,
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
      await click(mediaCard.getByRole('button').filter({ hasText: orderId }));
    };
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
      card.getByRole('button', { name: 'Добавить: Препати', exact: true }),
    );
    await waitText(card, '35,00 BYN');
    await click(
      card.getByRole('button', { name: 'Отправить чек', exact: true }),
    );
    await page
      .locator('#messages')
      .getByText('Пришлите чек документом', { exact: false })
      .waitFor();
    await upload(orderId);
    await waitText(card, 'Статус: Чек на проверке');
    assert.equal(await card.getAttribute('data-message-id'), messageId);
    assert.equal(
      await card
        .getByRole('button', { name: 'Удалить заказ', exact: true })
        .count(),
      0,
    );
    await click(
      card.getByRole('button', { name: 'Оплата BE: Борис', exact: true }),
    );
    await waitText(card, 'Оплата: BE');
    await page.locator('#user').selectOption('202');
    await click(page.locator('#orders'));
    const inbox = page
      .locator('.message[data-bot="true"]')
      .filter({ hasText: 'Проверка оплаты' })
      .filter({ hasText: orderId });
    await waitText(inbox, 'Оплата: BE');
    const documents = page
      .locator('.message[data-bot="true"] a')
      .filter({ hasText: 'Документ: receipt.pdf' });
    const previousDocuments = await documents.count();
    await click(
      inbox.getByRole('button', { name: 'Открыть чек', exact: true }),
    );
    const forwarded = documents.nth(previousDocuments);
    await forwarded.waitFor();
    const [download] = await Promise.all([
      page.waitForEvent('download'),
      click(forwarded),
    ]);
    assert.equal(download.suggestedFilename(), 'receipt.pdf');
    const chunks = await Array.fromAsync(await download.createReadStream());
    assert.deepEqual(Buffer.concat(chunks), receipt);
    await forwarded.scrollIntoViewIfNeeded();
    await page.screenshot({
      path: `test-results/proofs-${touch ? 'touch' : 'mouse'}.png`,
    });
    await click(
      inbox.getByRole('button', { name: 'Отклонить оплату', exact: true }),
    );
    await inbox.waitFor({ state: 'detached' });
    await page.locator('#user').selectOption('101');
    await waitText(card, 'Статус: Не оплачен');
    await click(
      card.getByRole('button', { name: 'Отправить чек', exact: true }),
    );
    await page
      .locator('#messages')
      .getByText('Пришлите чек документом', { exact: false })
      .waitFor();
    await upload(orderId);
    await waitText(card, 'Статус: Чек на проверке');
    await click(
      card.getByRole('button', { name: 'Отменить чек', exact: true }),
    );
    await waitText(card, 'Статус: Не оплачен');
    await click(
      card.getByRole('button', { name: 'Отправить чек', exact: true }),
    );
    await page
      .locator('#messages')
      .getByText('Пришлите чек документом', { exact: false })
      .waitFor();
    await upload(orderId);
    await waitText(card, 'Статус: Чек на проверке');
    await page.locator('#user').selectOption('202');
    await click(
      inbox.getByRole('button', { name: 'Принять оплату', exact: true }),
    );
    await inbox.waitFor({ state: 'detached' });
    await page.locator('#user').selectOption('101');
    await waitText(card, 'Статус: Оплачен');
    assert.deepEqual(errors, []);
    await context.close();
  }
  console.log(
    'Proof browser checks passed: mouse/touch, upload, country, exact-byte download, reject/cancel/resubmit/accept.',
  );
} finally {
  await browser.close();
}

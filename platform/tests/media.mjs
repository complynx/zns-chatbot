import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { QARun } from '../scripts/fqa/run.mjs';
import { openTelegram } from '../scripts/fqa/browser.mjs';
import { operationKey, waitFor } from '../scripts/fqa/client.mjs';

// Valid synthetic PNG. Captions drive the fixture; this is not a vision test.
const image = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=',
  'base64',
);
const languages = [
  {
    language: 'ru',
    touch: false,
    saved:
      'Чек отправлен на проверку. Текущий статус оплаты указан в карточке заказа.',
    avatar: 'Обработка аватарок сейчас недоступна. Файл не отправлен как чек.',
    choose: 'Выберите заказ для этого чека. Оплата ещё не подтверждена.',
    stale:
      'Заказ или ваши права изменились. Выберите актуальный заказ или отправьте файл снова.',
  },
  {
    language: 'en',
    touch: true,
    saved:
      'Receipt submitted for review. See the order card for the current payment status.',
    avatar:
      'Avatar processing is currently unavailable. This file was not submitted as a receipt.',
    choose: 'Select the order for this receipt. No payment has been approved.',
    stale:
      'The order or your access changed. Select a current order or send the file again.',
  },
];

async function upload(run, ui, caption, subject = 'alice') {
  console.log(`Uploading photo for ${subject}: ${caption}`);
  const before = await run.client.state(subject);
  const cursor = before.processed_cursor;
  await ui.page.locator('#upload-kind').selectOption('photo');
  await ui.page.locator('#document').setInputFiles({
    name: 'synthetic.png',
    mimeType: 'image/png',
    buffer: image,
  });
  await ui.page.locator('#caption').fill(caption);
  const [response] = await Promise.all([
    ui.page.waitForResponse(
      (result) =>
        new URL(result.url()).pathname === '/lab/photo' &&
        result.request().method() === 'POST',
    ),
    ui.click(ui.page.locator('#send-document')),
  ]);
  assert.equal(response.status(), 200);
  // Some Edge builds leave upload response.body() pending after delivery.
  // Correlate the actual new Telegram photo instead of waiting on that body.
  const state = await waitFor(
    () => run.client.state(subject),
    (value) =>
      value.messages.some(
        (message) =>
          message.message_id >= cursor &&
          message.caption === caption &&
          message.photo?.length,
      ),
    { label: 'uploaded photo in Telegram' },
  );
  const photo = state.messages.findLast(
    (message) =>
      message.message_id >= cursor &&
      message.caption === caption &&
      message.photo?.length,
  );
  const update = { update_id: photo.message_id };
  await run.client.processed(subject, update.update_id);
  return update;
}

async function current(run, order) {
  return run.client.order('alice', order.event_id, order.id);
}

async function change(run, order, name, extra = {}) {
  const latest = await current(run, order);
  const result = await run.client.action('alice', {
    event_id: latest.event_id,
    order_id: latest.id,
    version: latest.version,
    attempt: latest.attempt,
    name,
    origin: 'manual',
    key: operationKey(),
    ...extra,
  });
  assert.equal(result.status, 200);
  return result.body;
}

async function assertUnpaid(run, order) {
  const latest = await current(run, order);
  assert.equal(latest.state, 'unpaid');
}

function botText(ui, text) {
  return ui.page.locator('.message[data-bot="true"]').filter({ hasText: text });
}

for (const labels of languages) {
  const run = await QARun.create('media-' + randomUUID());
  let ui;
  let visitor;
  let preference;
  try {
    preference = await run.client.json('api', '/v1/me/preferences');
    const existing = await run.client.orders();
    const unchanged = existing.map((order) => ({
      id: order.id,
      version: order.version,
      state: order.state,
    }));
    // The 90 BYN choice is canonicalized by Core. Never clear someone else's
    // unpaid orders merely to make receipt matching unambiguous.
    const choice = {
      extras: {
        excursion_minsk: 0,
        excursion_grodno_overview: 0,
        preparty: 0,
      },
    };
    const first = await run.createOrder('alice', choice);
    assert.ok(first.choice.total > 0);
    assert.ok(
      existing.every(
        (order) =>
          !(
            ['unpaid', 'cash'].includes(order.state) &&
            order.choice.total === first.choice.total
          ),
      ),
      'This stand already has a matching unpaid amount; use an isolated stand. Existing orders were preserved.',
    );
    ui = await openTelegram(run, { touch: labels.touch });
    ui.page.setDefaultTimeout(15_000);
    console.log(`Media ${labels.language}: browser ready`);
    await ui.send('/language ' + labels.language);
    await ui.send('/orders');
    const receiptCaption = `receipt ${first.choice.total.toFixed(2)} BYN`;

    // An unsolicited, uniquely priced receipt enters review, never paid.
    await upload(run, ui, receiptCaption);
    await botText(ui, labels.saved).last().waitFor();
    const reviewed = await waitFor(
      () => current(run, first),
      (order) => order.state === 'proof',
      { label: 'unsolicited receipt under review' },
    );
    assert.ok(reviewed.proof_file);
    await run.save('unique-receipt.json', reviewed);
    await change(run, first, 'cancel_proof');
    await ui.send('/orders');

    // A pending explicit proof hint cannot turn an avatar into a receipt.
    const orderCard = botText(ui, first.id).filter({
      has: ui.page.getByRole('button', { name: 'Отправить чек', exact: true }),
    });
    await ui.act(() =>
      ui.click(
        orderCard.getByRole('button', { name: 'Отправить чек', exact: true }),
      ),
    );
    const beforeAvatar = await current(run, first);
    await upload(run, ui, 'avatar');
    await botText(ui, labels.avatar).last().waitFor();
    assert.deepEqual(await current(run, first), beforeAvatar);

    // Same-price orders require explicit selection. An unrelated question must
    // neither attach the receipt nor invalidate the existing choice buttons.
    const second = await run.createOrder('alice', choice);
    await ui.send('/orders');
    await upload(run, ui, receiptCaption);
    const choices = botText(ui, labels.choose).last();
    await choices.waitFor();
    await assertUnpaid(run, first);
    await assertUnpaid(run, second);
    await choices.getByRole('button').filter({ hasText: first.id }).waitFor();
    await choices.getByRole('button').filter({ hasText: second.id }).waitFor();
    await ui.send('payment instructions');
    await assertUnpaid(run, first);
    await assertUnpaid(run, second);
    await ui.act(() =>
      ui.click(choices.getByRole('button').filter({ hasText: second.id })),
    );
    await waitFor(
      () => current(run, second),
      (order) => order.state === 'proof',
    );
    await assertUnpaid(run, first);
    await change(run, second, 'cancel_proof');

    // A captured choice must not attach after its order version changes.
    await upload(run, ui, receiptCaption);
    const staleChoices = botText(ui, labels.choose).last();
    await staleChoices
      .getByRole('button')
      .filter({ hasText: first.id })
      .waitFor();
    await change(run, first, 'edit', { choice: { extras: { preparty: 0 } } });
    await ui.act(() =>
      ui.click(staleChoices.getByRole('button').filter({ hasText: first.id })),
    );
    await botText(ui, labels.stale).last().waitFor();
    await assertUnpaid(run, first);
    await assertUnpaid(run, second);

    visitor = await openTelegram(run, {
      subject: 'visitor',
      touch: labels.touch,
    });
    const visitorBefore = await run.client.orders('visitor');
    const denied = await upload(run, visitor, receiptCaption, 'visitor');
    assert.deepEqual(await run.client.orders('visitor'), visitorBefore);
    await run.save('denied-photo.json', {
      update: denied,
      state: await run.client.state('visitor'),
    });
    const after = await run.client.orders();
    for (const previous of unchanged) {
      const order = after.find((item) => item.id === previous.id);
      assert.ok(order, 'An unrelated order disappeared');
      assert.equal(order.version, previous.version);
      assert.equal(order.state, previous.state);
    }
    await run.save('media-state.json', await run.client.state());
  } finally {
    try {
      await visitor?.close('visitor');
      if (ui && preference) await ui.send('/language ' + preference.language);
      await ui?.close();
    } finally {
      await run.cleanup({ cancelProofs: true });
    }
  }
  console.log(
    `Media ${labels.language} ${labels.touch ? 'touch' : 'mouse'} passed`,
  );
}

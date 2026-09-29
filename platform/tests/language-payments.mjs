import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { QARun } from '../scripts/fqa/run.mjs';
import { openTelegram } from '../scripts/fqa/browser.mjs';
import { waitFor } from '../scripts/fqa/client.mjs';

for (const touch of [false, true]) {
  const run = await QARun.create('language-payments-' + randomUUID());
  let ui;
  let preference;
  try {
    preference = await run.client.json('api', '/v1/me/preferences');
    const order = await run.createOrder('alice', { extras: { preparty: 0 } });
    ui = await openTelegram(run, { touch });
    await ui.send('/language en');
    await ui.send('/orders');
    let state = await run.client.state();
    const orderCard = state.messages.find((item) =>
      item.text.startsWith('Заказ ' + order.id),
    );
    assert.ok(orderCard);
    await ui.act(() =>
      ui.click(
        ui.card(orderCard.message_id).getByRole('button', {
          name: 'Payment methods',
          exact: true,
        }),
      ),
    );
    state = await waitFor(
      () => run.client.state(),
      (value) =>
        value.messages.some((item) =>
          item.text.startsWith('Payment for order ' + order.id),
        ),
      { label: 'English payment card' },
    );
    const payment = state.messages.find((item) =>
      item.text.startsWith('Payment for order ' + order.id),
    );
    assert.ok(payment.text.includes('TEST PAYMENT DETAILS'));
    assert.ok(payment.text.includes('1,050.00'));
    const contact = ui.card(payment.message_id).getByRole('button', {
      name: /Борис/,
    });
    await ui.click(contact);
    await ui.page
      .getByText('Тестовый переход к контакту: tg://user?id=202', {
        exact: true,
      })
      .waitFor();
    state = await run.client.state();
    const language = state.messages.find((item) =>
      item.text.includes('Current language:'),
    );
    assert.ok(language);
    await ui.act(() =>
      ui.click(
        ui.card(language.message_id).getByRole('button', {
          name: 'ru',
          exact: true,
        }),
      ),
    );
    await ui
      .card(payment.message_id)
      .getByText(/ТЕСТОВЫЕ РЕКВИЗИТЫ/)
      .waitFor();
    const current = await run.client.order('alice', order.event_id, order.id);
    assert.equal(current.version, order.version);
    assert.equal(current.state, 'unpaid');
    await run.save('localized-cards.json', await run.client.state());
  } finally {
    try {
      if (ui && preference) await ui.send('/language ' + preference.language);
      await ui?.close();
    } finally {
      await run.cleanup();
    }
  }
  console.log(`Payment language ${touch ? 'touch' : 'mouse'} passed`);
}

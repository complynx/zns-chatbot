import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { QARun } from '../scripts/fqa/run.mjs';
import { openTelegram } from '../scripts/fqa/browser.mjs';

for (const touch of [false, true]) {
  const run = await QARun.create('browser-' + randomUUID());
  let ui;
  try {
    const owned = await run.createOrder('alice', { extras: { preparty: 0 } });
    ui = await openTelegram(run, { touch });
    await ui.send('/orders');
    const state = await run.client.state();
    const message = state.messages.find((item) =>
      item.text.startsWith('Заказ ' + owned.id),
    );
    assert.ok(message);
    await ui.card(message.message_id).waitFor();
    await ui.act(() =>
      ui.click(
        ui
          .card(message.message_id)
          .getByRole('button', { name: 'Добавить: Трансфер', exact: true }),
      ),
    );
    const changed = await run.client.order('alice', owned.event_id, owned.id);
    assert.equal(changed.choice.total, 100);
    let previous = 0;
    for (let index = 0; index < 2; index++) {
      const update = await ui.send('/exportfoodorders');
      assert.ok(update.update_id > previous);
      previous = update.update_id;
      const denied = await run.client.state();
      const reply = denied.history.find(
        (item) =>
          item.update_id === update.update_id && item.kind === 'orders_reply',
      );
      assert.ok(reply?.content.includes('forbidden'));
      await run.save(`denied-${index}.json`, { update, state: denied });
    }
  } finally {
    try {
      await ui?.close();
    } finally {
      await run.cleanup();
    }
  }
  console.log(`FQA kit ${touch ? 'touch' : 'mouse'} passed: ${run.directory}`);
}

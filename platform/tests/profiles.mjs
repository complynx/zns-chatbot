import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { QARun } from '../scripts/fqa/run.mjs';
import { openTelegram } from '../scripts/fqa/browser.mjs';

for (const touch of [false, true]) {
  const run = await QARun.create('profiles-' + randomUUID());
  let ui;
  let preference;
  try {
    preference = await run.client.json('api', '/v1/me/preferences');
    ui = await openTelegram(run, { touch });
    await ui.send('/language en');
    await ui.send('/profile');
    let state = await run.client.state();
    const card = state.messages.find((item) =>
      item.reply_markup?.inline_keyboard?.some((row) =>
        row.some((button) => button.callback_data?.startsWith('profile:name:')),
      ),
    );
    assert.ok(card);
    await ui.act(() =>
      ui.click(
        ui.card(card.message_id).getByRole('button', {
          name: 'Set full name',
          exact: true,
        }),
      ),
    );
    const before = await run.client.json('api', '/v1/me/pass-profile');
    assert.equal(before.pending, 'legal_name');
    await ui.send('payment instructions');
    assert.deepEqual(
      await run.client.json('api', '/v1/me/pass-profile'),
      before,
    );
    await ui.send('My name is Avery Example');
    let current = await run.client.json('api', '/v1/me/pass-profile');
    assert.equal(current.legal_name, 'Avery Example');
    assert.equal(current.pending, '');
    await ui
      .card(card.message_id)
      .getByText(/Avery Example/)
      .waitFor();
    await ui.send('/language ru');
    await ui
      .card(card.message_id)
      .getByRole('button', {
        name: 'Указать полное имя',
        exact: true,
      })
      .waitFor();
    await ui.send('Меня зовут Иван Примеров');
    current = await run.client.json('api', '/v1/me/pass-profile');
    assert.equal(current.legal_name, 'Иван Примеров');
    assert.equal(current.pending, '');
    await ui
      .card(card.message_id)
      .getByText(/Иван Примеров/)
      .waitFor();
    state = await run.client.state();
    await run.save('profile-flow.json', { before, current, state });
  } finally {
    try {
      if (ui && preference) await ui.send('/language ' + preference.language);
      await ui?.close();
    } finally {
      await run.cleanup();
    }
  }
  console.log(`Profile ${touch ? 'touch' : 'mouse'} passed`);
}

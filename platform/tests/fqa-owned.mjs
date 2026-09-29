import assert from 'node:assert/strict';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { Evidence } from '../scripts/fqa/evidence.mjs';
import { OwnedStand, executeJoined } from '../scripts/fqa/stand.mjs';
import { QARun } from '../scripts/fqa/run.mjs';
import { waitFor } from '../scripts/fqa/client.mjs';

function errorRecord(error) {
  return {
    message: String(error),
    stack: error.stack,
    ...(error.unjoined && { unjoined: true, ownedPid: error.ownedPid }),
    ...(error.cause && { cause: errorRecord(error.cause) }),
    ...(error.errors && {
      errors: [...error.errors].map((error) => errorRecord(error)),
    }),
  };
}

// Only this child entrypoint imports Playwright. The suite owns its PID before import.
async function browserPhase(phase, name) {
  if (!['before', 'after'].includes(phase))
    throw new Error('Invalid browser phase');
  const descriptor = JSON.parse(await new Evidence(name).read('phase.json'));
  const { endpoints, locale, touch, cell, candidate } = descriptor;
  const run = await QARun.resume(name, endpoints);
  const { openTelegram } = await import('../scripts/fqa/browser.mjs');
  const label = cell + '-' + phase;
  let ui;
  let failure;
  try {
    if (phase === 'before') {
      const order = await run.createOrder('alice', {
        extras: { preparty: 0 },
      });
      ui = await openTelegram(run, {
        touch,
        label,
      });
      await ui.send('/language ' + locale);
      const preference = await run.client.json('api', '/v1/me/preferences');
      assert.equal(preference.language, locale);
      await ui.send('/orders');
      const state = await run.client.state();
      const prefix = locale === 'en' ? 'Order ' : 'Заказ ';
      const message = state.messages.find((item) =>
        item.text.startsWith(prefix + order.id),
      );
      assert.ok(message, 'Owned order must appear in selected language');
      const card = ui.card(message.message_id);
      const add = locale === 'en' ? 'Add: Shuttle' : 'Добавить: Трансфер';
      await ui.act(() =>
        ui.click(card.getByRole('button', { name: add, exact: true })),
      );
      const changed = await run.client.order('alice', order.event_id, order.id);
      assert.equal(changed.version, order.version + 1);
      assert.equal(changed.choice.total, 100);
      const remove = locale === 'en' ? 'Remove: Shuttle' : 'Убрать: Трансфер';
      await card.getByRole('button', { name: remove, exact: true }).waitFor();
      const foreign = await run.client.request(
        'api',
        `/v1/order-events/${order.event_id}/orders/${order.id}`,
        { subject: 'visitor' },
      );
      assert.ok(
        [403, 404].includes(foreign.status),
        'Foreign order read must be denied',
      );
      await run.save('before-restart.json', {
        candidate,
        cell,
        order: changed,
        initialTotal: order.choice.total,
        message,
      });
    } else {
      const saved = JSON.parse(await run.evidence.read('before-restart.json'));
      const { order: changed, message, initialTotal } = saved;
      const order = changed;
      ui = await openTelegram(run, { touch, label });
      const card = ui.card(message.message_id);
      const add = locale === 'en' ? 'Add: Shuttle' : 'Добавить: Трансфер';
      const remove = locale === 'en' ? 'Remove: Shuttle' : 'Убрать: Трансфер';
      await waitFor(
        () => run.client.state(),
        (value) =>
          value.messages.some((item) => item.message_id === message.message_id),
        { label: 'persisted Telegram card' },
      );
      assert.deepEqual(
        await run.client.order('alice', order.event_id, order.id),
        changed,
      );
      const persistedPreference = await run.client.json(
        'api',
        '/v1/me/preferences',
      );
      assert.equal(persistedPreference.language, locale);

      await card.getByRole('button', { name: remove, exact: true }).waitFor();
      await ui.act(() =>
        ui.click(card.getByRole('button', { name: remove, exact: true })),
      );
      const restored = await run.client.order(
        'alice',
        order.event_id,
        order.id,
      );
      assert.equal(restored.version, changed.version + 1);
      assert.equal(restored.choice.total, initialTotal);
      await card.getByRole('button', { name: add, exact: true }).waitFor();
      await run.save('after-restart.json', {
        candidate,
        cell,
        order: restored,
        state: await run.client.state(),
        physicalTouch: false,
      });
    }
  } catch (error) {
    failure = error;
  } finally {
    if (ui) {
      try {
        await ui.close(label);
        assert.deepEqual(
          JSON.parse(await run.evidence.read(label + '-browser-errors.json')),
          [],
          'No browser errors, including initial navigation',
        );
      } catch (error) {
        failure = failure
          ? new AggregateError(
              [failure, error],
              'Scenario and browser cleanup failed',
              { cause: failure },
            )
          : error;
      }
    }
  }
  await run.save(phase + '-result.json', {
    status: failure ? 'failed' : 'passed',
    error: failure && errorRecord(failure),
  });
  if (failure) throw failure;
}

export async function runPhase(stand, run, name, phase) {
  const started = new Date().toISOString();
  let failure;
  let output;
  try {
    output = await executeJoined(
      process.execPath,
      [fileURLToPath(import.meta.url), '--browser-phase', phase, name],
      {
        signal: stand.signal,
        timeout: 60_000,
        encoding: 'utf8',
        maxBuffer: 8 * 1024 * 1024,
      },
    );
  } catch (error) {
    // Retain ownership before artifact failures can wrap the process error.
    if (error.unjoined) stand.unjoinedChild = error;
    failure = error;
  }
  try {
    await run.save(phase + '-process.json', {
      started,
      finished: new Date().toISOString(),
      status: failure ? 'failed' : 'passed',
      output: output ?? failure?.output,
      error: failure && errorRecord(failure),
    });
  } catch (error) {
    failure = failure
      ? new AggregateError([failure, error], 'Phase and evidence failed', {
          cause: failure,
        })
      : error;
  }
  if (failure) throw failure;
}

async function suite() {
  // Playwright detaches Unix browsers; group kill alone cannot contain those trees.
  if (process.platform !== 'win32')
    throw new Error(
      'Owned browser suite currently requires Windows process-tree containment',
    );
  const stand = new OwnedStand(process.env.FQA_IMAGE);
  const results = [];
  let failure;
  const cleanupErrors = [];
  const deadline = setTimeout(
    () => stand.controller.abort(new Error('FQA suite deadline exceeded')),
    300_000,
  );
  try {
    let endpoints = await stand.start();
    for (const locale of ['en', 'ru']) {
      for (const touch of [false, true]) {
        stand.signal.throwIfAborted();
        const cell = `${locale}-${touch ? 'touch' : 'mouse'}`;
        const run = await QARun.create(`${stand.project}-${cell}`, endpoints);
        const name = `${stand.project}-${cell}`;
        const descriptor = {
          endpoints,
          locale,
          touch,
          cell,
          candidate: stand.image,
        };
        await run.save('phase.json', descriptor);
        try {
          await runPhase(stand, run, name, 'before');
          endpoints = await stand.restart();
          descriptor.endpoints = endpoints;
          await run.save('phase.json', descriptor);
          await runPhase(stand, run, name, 'after');
        } catch (error) {
          results.push({
            cell,
            status: 'failed',
            error: errorRecord(error),
            artifacts: run.directory,
          });
          throw error;
        }
        results.push({ cell, status: 'passed', artifacts: run.directory });
      }
    }
  } catch (error) {
    failure = error;
  } finally {
    clearTimeout(deadline);
    try {
      await stand.close();
    } catch (error) {
      cleanupErrors.push(error);
    }
    try {
      await stand.evidence.write(
        'results.json',
        JSON.stringify(
          {
            candidate: stand.image,
            results,
            failure: failure && errorRecord(failure),
            cleanupErrors: cleanupErrors.map((error) => errorRecord(error)),
          },
          undefined,
          2,
        ),
      );
    } catch (error) {
      cleanupErrors.push(error);
    }
  }
  if (failure || cleanupErrors.length > 0)
    throw new AggregateError(
      [...(failure ? [failure] : []), ...cleanupErrors],
      'FQA failed',
      { cause: failure },
    );
  stand.signal.throwIfAborted();
  console.log(`Owned FQA passed: ${stand.evidence.directory}`);
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  if (process.argv[2] === '--browser-phase') {
    // Console signals must not remove the tree owner before the parent joins it.
    process.on('SIGINT', () => {});
    process.on('SIGTERM', () => {});
    try {
      await browserPhase(process.argv[3], process.argv[4]);
    } catch (error) {
      // Keep the tree owner alive for the parent's bounded taskkill /T on any failure.
      console.error(error);
      setInterval(() => {}, 1000);
    }
  } else await suite();
}

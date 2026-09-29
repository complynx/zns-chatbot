import assert from 'node:assert/strict';
import test from 'node:test';
import http from 'node:http';
import { createHmac, randomUUID } from 'node:crypto';
import { localBase, SandboxClient, waitFor } from './client.mjs';
import { QARun } from './run.mjs';
import { Evidence } from './evidence.mjs';
import { fixtureArguments } from './fixture.mjs';

test('custom stand signing key authenticates without entering evidence', async (t) => {
  const signingKey = 'synthetic-custom-stand-only';
  const records = [];
  const server = http.createServer((request, response) => {
    const [body, signature] = request.headers.authorization
      .slice(7)
      .split('.', 2);
    const expected = createHmac('sha256', signingKey)
      .update(body)
      .digest('base64url');
    response.writeHead(signature === expected ? 200 : 401, {
      'Content-Type': 'application/json',
    });
    response.end('{"ok":true}');
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => server.close());
  const client = new SandboxClient({
    api: `http://127.0.0.1:${server.address().port}`,
    signingKey,
    record: async (entry) => {
      records.push(entry);
    },
  });
  const result = await client.request('api', '/fixture');
  assert.equal(result.status, 200);
  assert.equal(records.length, 1);
  assert.ok(!JSON.stringify(records).includes(signingKey));
  assert.ok(!JSON.stringify(client).includes(signingKey));
});

test('fixture credentials stay on literal loopback; evidence paths stay in a run', () => {
  for (const value of [
    'https://example.com',
    'http://localhost:8091',
    'https://127.0.0.1@evil.test',
    'http://127.0.0.1/x',
    'http://127.0.0.1/?x',
  ])
    assert.throws(() => localBase(value));
  assert.equal(localBase('http://127.0.0.1:8091'), 'http://127.0.0.1:8091');
  for (const name of ['..', '../other', 'x/y', String.raw`x\y`]) {
    assert.throws(() => new Evidence(name));
    assert.throws(() => new Evidence('test').file(name));
  }
  assert.throws(() =>
    fixtureArguments('fixture', { EXPORT_FIXTURE_BATCH_COUNT: '1' }),
  );
  assert.deepEqual(
    fixtureArguments('fixture', {
      ORDER_FIXTURE_ID: 'own',
      ORDER_FIXTURE_AGE: '49h',
    }),
    [
      'run',
      '--rm',
      '--no-deps',
      '-e',
      'ORDER_FIXTURE_ID',
      '-e',
      'ORDER_FIXTURE_AGE',
      'migrate',
      'fixture',
    ],
  );
});

test('waits reject unchanged old evidence and retain the final sample', async () => {
  let sequence = 0;
  const result = await waitFor(
    async () => ({ revision: sequence++ }),
    (value) => value.revision > 1,
    { interval: 1 },
  );
  assert.equal(result.revision, 2);
  await assert.rejects(
    waitFor(
      async () => ({ old: true }),
      () => false,
      { timeout: 1, interval: 1, label: 'new update' },
    ),
    (error) => error.message.includes('new update') && error.cause.old,
  );
});

test('public client pagination, negative responses, replay recovery and owned cleanup', async (t) => {
  const orders = new Map();
  const creates = new Map();
  const commands = [];
  const server = http.createServer(async (request, response) => {
    response.setHeader('Content-Type', 'application/json');
    const reply = (status, data) => {
      response.writeHead(status);
      response.end(JSON.stringify(data));
    };
    if (request.url === '/denied') {
      reply(403, { code: 'forbidden' });
      return;
    }
    if (request.url === '/redirect') {
      response.writeHead(302, { Location: 'https://example.invalid' });
      response.end();
      return;
    }
    if (request.url.includes('/orders?')) {
      const cursor = new URL(request.url, 'http://127.0.0.1').searchParams.get(
        'cursor',
      );
      reply(200, {
        orders: [{ id: cursor ? 'second' : 'first' }],
        next: cursor ? '' : 'first',
      });
      return;
    }
    if (request.method === 'GET') {
      const order = orders.get(request.url.split('/').at(-1));
      reply(order ? 200 : 404, order || { code: 'order_not_found' });
      return;
    }
    const body = JSON.parse(
      Buffer.concat(await Array.fromAsync(request)).toString(),
    );
    commands.push(body);
    if (body.name === 'create') {
      if (body.choice.invalid) {
        reply(400, { code: 'invalid_choice' });
        return;
      }
      if (!creates.has(body.key)) {
        const order = {
          id: randomUUID(),
          event_id: body.event_id,
          state: 'unpaid',
          version: 1,
          attempt: '',
        };
        creates.set(body.key, order);
        orders.set(order.id, order);
      }
      reply(200, creates.get(body.key));
      return;
    }
    const order = orders.get(body.order_id);
    if (body.version !== order.version || body.attempt !== order.attempt) {
      reply(409, { code: 'stale_attempt' });
      return;
    }
    if (body.name === 'cancel_proof') {
      order.state = 'unpaid';
      order.version++;
      order.attempt = '';
      reply(200, order);
      return;
    }
    if (body.name === 'delete' && ['unpaid', 'cash'].includes(order.state)) {
      orders.delete(order.id);
      reply(200, { ...order, state: 'deleted' });
      return;
    }
    reply(409, { code: 'payment_locked' });
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  const api = 'http://127.0.0.1:' + server.address().port;
  const client = new SandboxClient({ api });
  const pages = await client.orders();
  assert.deepEqual(
    pages.map((order) => order.id),
    ['first', 'second'],
  );
  const denial = await client.request('api', '/denied', { binary: true });
  assert.equal(denial.body.code, 'forbidden');
  await assert.rejects(client.request('api', '/redirect'));
  await assert.rejects(client.request('api', '//example.invalid'));
  const name = 'contract-' + randomUUID();
  const run = await QARun.create(name, { api });
  const owned = await run.createOrder();
  delete run.manifest.orders[0].id;
  await run.persist();
  const resumed = await QARun.resume(name, { api });
  const recovered = await resumed.recoverOrder(resumed.manifest.orders[0]);
  assert.equal(recovered.id, owned.id);
  await assert.rejects(resumed.createOrder('alice', { invalid: true }));
  const paid = await resumed.createOrder();
  orders.get(paid.id).state = 'paid';
  const proof = await resumed.createOrder();
  Object.assign(orders.get(proof.id), {
    state: 'proof',
    version: 7,
    attempt: 'current-attempt',
  });
  orders.set('other-run', { id: 'other-run', state: 'unpaid' });
  const first = await resumed.cleanup();
  assert.equal(first.find((item) => item.id === proof.id).retained, true);
  const cleanup = await resumed.cleanup({ cancelProofs: true });
  assert.equal(cleanup.find((item) => item.id === paid.id).retained, true);
  assert.equal(cleanup.find((item) => item.id === proof.id).state, 'deleted');
  assert.equal(orders.has('other-run'), true);
  assert.equal(
    commands.find((command) => command.name === 'cancel_proof').attempt,
    'current-attempt',
  );
  assert.equal(
    commands.filter((command) => command.order_id === paid.id).length,
    0,
  );
  assert.equal(commands.filter((command) => command.choice?.invalid).length, 1);
});

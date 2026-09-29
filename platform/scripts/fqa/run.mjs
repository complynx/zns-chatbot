import { Evidence } from './evidence.mjs';
import { randomUUID } from 'node:crypto';
import { operationKey, SandboxClient } from './client.mjs';

export class QARun {
  static async create(directory, options) {
    const evidence = new Evidence(directory);
    await evidence.init();
    const manifest = { id: randomUUID(), orders: [] };
    await evidence.write(
      'manifest.json',
      JSON.stringify(manifest, undefined, 2),
      'wx',
    );
    return new QARun(directory, manifest, options);
  }

  static async resume(directory, options) {
    const manifest = JSON.parse(
      await new Evidence(directory).read('manifest.json'),
    );
    if (typeof manifest.id !== 'string' || !Array.isArray(manifest.orders))
      throw new Error('Invalid QA manifest');
    return new QARun(directory, manifest, options);
  }

  constructor(directory, manifest, options) {
    this.evidence = new Evidence(directory);
    this.directory = this.evidence.directory;
    this.manifest = manifest;
    this.client = new SandboxClient({
      ...options,
      record: (entry) =>
        this.evidence.write(
          'requests.jsonl',
          JSON.stringify({ time: new Date().toISOString(), ...entry }) + '\n',
          'a',
        ),
    });
  }

  artifact(name) {
    return this.evidence.file(name);
  }
  async save(name, value) {
    await this.evidence.write(name, JSON.stringify(value, undefined, 2));
  }
  async persist() {
    await this.save('manifest.next.json', this.manifest);
    await this.evidence.replace('manifest.next.json', 'manifest.json');
  }

  async createOrder(
    subject = 'alice',
    choice = {},
    event = 'sandbox-festival',
  ) {
    // Persist intent before the request. A lost response can be recovered with the same key.
    const entry = {
      subject,
      event,
      command: {
        event_id: event,
        name: 'create',
        version: 0,
        origin: 'manual',
        key: operationKey(),
        choice,
      },
    };
    this.manifest.orders.push(entry);
    await this.persist();
    return this.recoverOrder(entry);
  }

  async recoverOrder(entry) {
    if (entry.rejected)
      throw new Error('Fixture creation was definitively rejected');
    const result = await this.client.action(entry.subject, entry.command);
    if (
      result.status >= 400 &&
      result.status < 500 &&
      ![408, 429].includes(result.status)
    ) {
      entry.rejected = { status: result.status, body: result.body };
      await this.persist();
    }
    if (result.status !== 200)
      throw new Error('Fixture creation failed', { cause: result });
    entry.id = result.body.id;
    await this.persist();
    return result.body;
  }

  async cleanup({ cancelProofs = false } = {}) {
    const results = [];
    for (const entry of this.manifest.orders) {
      if (entry.rejected) {
        results.push({
          key: entry.command.key,
          state: 'creation_rejected',
          status: entry.rejected.status,
        });
        continue;
      }
      if (!entry.id) await this.recoverOrder(entry);
      const route = `/v1/order-events/${encodeURIComponent(entry.event)}/orders/${encodeURIComponent(entry.id)}`;
      let result = await this.client.request('api', route, {
        subject: entry.subject,
      });
      if (result.status === 404) {
        results.push({ id: entry.id, state: 'absent' });
        continue;
      }
      if (result.status !== 200)
        throw new Error('Cannot inspect owned fixture', { cause: result });
      let order = result.body;
      if (cancelProofs && order.state === 'proof') {
        result = await this.client.action(
          entry.subject,
          cleanupCommand(order, 'cancel_proof'),
        );
        if (result.status !== 200)
          throw new Error('Proof cleanup rejected', { cause: result });
        order = result.body;
      }
      if (['unpaid', 'cash'].includes(order.state)) {
        result = await this.client.action(
          entry.subject,
          cleanupCommand(order, 'delete'),
        );
        if (result.status !== 200)
          throw new Error('Order cleanup rejected', { cause: result });
        results.push({ id: entry.id, state: 'deleted' });
      } else {
        results.push({ id: entry.id, state: order.state, retained: true });
      }
      await this.save('cleanup.json', results);
    }
    await this.save('cleanup.json', results);
    return results;
  }
}

function cleanupCommand(order, name) {
  return {
    event_id: order.event_id,
    order_id: order.id,
    version: order.version,
    attempt: order.attempt,
    name,
    origin: 'manual',
    key: operationKey(),
  };
}

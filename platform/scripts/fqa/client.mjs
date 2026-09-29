import { createHmac, randomUUID } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';

const subjects = new Map([
  ['alice', 101],
  ['bob', 202],
  ['visitor', 303],
]);
const defaultSigningKey = 'sandbox-only-do-not-use-in-production-123456789';

// Fixture credentials must never be sent to a remote or redirected endpoint.
export function localBase(value) {
  const url = new URL(value);
  if (
    url.protocol !== 'http:' ||
    !['127.0.0.1', '[::1]'].includes(url.hostname) ||
    url.username ||
    url.password ||
    url.pathname !== '/' ||
    url.search ||
    url.hash
  ) {
    throw new Error('FQA requires an explicit loopback HTTP origin');
  }
  return url.origin;
}

export function userID(subject) {
  if (!subjects.has(subject)) throw new Error('Unknown sandbox subject');
  return subjects.get(subject);
}

export function operationKey() {
  return `fqa-${randomUUID()}`;
}

function token(subject, signingKey) {
  userID(subject);
  const exp = Math.floor(Date.now() / 1000) + 60;
  const body = Buffer.from(
    JSON.stringify({
      sub: subject,
      actor: 'sandbox-bot',
      aud: 'zns-core',
      exp,
    }),
  ).toString('base64url');
  return (
    body +
    '.' +
    createHmac('sha256', signingKey).update(body).digest('base64url')
  );
}

export async function waitFor(
  sample,
  predicate,
  { timeout = 15_000, interval = 100, label = 'condition' } = {},
) {
  const deadline = Date.now() + timeout;
  let last;
  do {
    last = await sample();
    if (await predicate(last)) return last;
    await delay(interval);
  } while (Date.now() < deadline);
  throw new Error(`Timed out waiting for ${label}`, { cause: last });
}

export class SandboxClient {
  #signingKey;

  constructor({
    ui = 'http://127.0.0.1:8090',
    api = 'http://127.0.0.1:8091',
    record = async () => {},
    signingKey = defaultSigningKey,
  } = {}) {
    this.ui = localBase(ui);
    this.api = localBase(api);
    this.record = record;
    this.#signingKey = signingKey;
  }

  async request(
    surface,
    route,
    { subject = 'alice', method = 'GET', body, binary = false } = {},
  ) {
    const base = surface === 'api' ? this.api : this.ui;
    if (
      !['api', 'ui'].includes(surface) ||
      !route.startsWith('/') ||
      new URL(route, base).origin !== base
    ) {
      throw new Error('Invalid sandbox request target');
    }
    const headers = { 'Content-Type': 'application/json' };
    if (surface === 'api')
      headers.Authorization = 'Bearer ' + token(subject, this.#signingKey);
    else headers['X-Sandbox'] = '1';
    const response = await fetch(base + route, {
      method,
      headers,
      redirect: 'error',
      signal: AbortSignal.timeout(10_000),
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const bytes = Buffer.from(await response.arrayBuffer());
    const type = response.headers.get('content-type') || '';
    const data = type.includes('application/json')
      ? JSON.parse(bytes.toString('utf8'))
      : binary
        ? bytes
        : bytes.toString('utf8');
    const result = {
      status: response.status,
      headers: Object.fromEntries(response.headers),
      body: data,
    };
    await this.record({
      surface,
      route,
      subject,
      method,
      request: body,
      response: Buffer.isBuffer(data)
        ? { ...result, body: { bytes: bytes.length } }
        : result,
    });
    return result;
  }

  async json(surface, route, options) {
    const result = await this.request(surface, route, options);
    if (result.status < 200 || result.status >= 300)
      throw new Error(`HTTP ${result.status}: ${route}`, { cause: result });
    return result.body;
  }

  state(subject = 'alice') {
    return this.json('ui', '/lab/state?user=' + userID(subject));
  }

  order(subject, event, id) {
    return this.json(
      'api',
      `/v1/order-events/${encodeURIComponent(event)}/orders/${encodeURIComponent(id)}`,
      { subject },
    );
  }

  async orders(subject = 'alice', event = 'sandbox-festival') {
    const result = [];
    const seen = new Set();
    let cursor = '';
    do {
      const page = await this.json(
        'api',
        `/v1/order-events/${encodeURIComponent(event)}/orders?cursor=${encodeURIComponent(cursor)}`,
        { subject },
      );
      result.push(...page.orders);
      cursor = page.next;
      if (cursor && seen.has(cursor))
        throw new Error('Repeated pagination cursor');
      seen.add(cursor);
    } while (cursor);
    return result;
  }

  // Exact caller command is preserved: stale versions, attempts and replay keys stay testable.
  action(subject, command) {
    return this.request('api', '/v1/order-actions', {
      subject,
      method: 'POST',
      body: command,
    });
  }

  processed(subject, updateID) {
    return waitFor(
      () => this.state(subject),
      (state) => state.processed_cursor > updateID,
      { label: `Telegram update ${updateID}` },
    );
  }
}

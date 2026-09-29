import assert from 'node:assert/strict';
import childProcess from 'node:child_process';
import { PassThrough } from 'node:stream';
import test from 'node:test';
import {
  isolatedConfig,
  OwnedStand,
  executeJoined,
  terminateOwnedTree,
} from './stand.mjs';
import { Evidence } from './evidence.mjs';
import { setTimeout as delay } from 'node:timers/promises';
import { randomUUID } from 'node:crypto';
import { chromium } from 'playwright';
import { openTelegram } from './browser.mjs';
import { runPhase } from '../../tests/fqa-owned.mjs';

const image = 'sha256:' + 'a'.repeat(64);
const postgresImage = 'sha256:' + 'b'.repeat(64);
const project = 'synthetic-qa-zns-12345678-1234-1234-1234-123456789abc';
const source = {
  name: 'shared',
  services: {
    postgres: {
      environment: { POSTGRES_DB: 'shared' },
      healthcheck: {},
      ports: [{ published: '55432' }],
    },
    api: {
      build: '.',
      environment: {
        DATABASE_URL: 'postgres://u:p@postgres/shared?sslmode=disable',
      },
    },
    fake: {},
    bot: {},
  },
  networks: { sandbox: { internal: true }, host: {} },
  volumes: { pgdata: { name: 'shared-data' } },
};

test('isolated stands do not inherit shared names, database or host ports', () => {
  const config = isolatedConfig(source, { project, image, postgresImage });
  assert.equal(source.name, 'shared');
  assert.equal(config.services.postgres.ports, undefined);
  assert.equal(config.services.api.build, undefined);
  assert.equal(config.services.postgres.image, postgresImage);
  assert.equal(config.services.bot.image, image);
  assert.equal(config.services.api.ports[0].host_ip, '127.0.0.1');
  assert.equal(config.services.api.ports[0].published, '0');
  assert.equal(
    new URL(config.services.api.environment.DATABASE_URL).pathname,
    '/' + project.replaceAll('-', '_'),
  );
  assert.equal(
    config.services.postgres.environment.POSTGRES_DB,
    project.replaceAll('-', '_'),
  );
  assert.equal(config.volumes.pgdata.name, project + '-pgdata');
  assert.equal(config.networks.sandbox.internal, true);
  assert.equal(config.services.bot.labels.synthetic, 'true');
  assert.equal(config.services.bot.container_name, project + '-bot');
});

test('mutable images and non-synthetic project names are rejected before Docker', () => {
  assert.throws(() => new OwnedStand('zns-sandbox:local'));
  assert.throws(() =>
    isolatedConfig(source, { project: 'zns-sandbox', image, postgresImage }),
  );
  assert.throws(() =>
    isolatedConfig(source, { project, image: 'latest', postgresImage }),
  );
});

test('independent invocations own distinct resource namespaces', () => {
  const first = new OwnedStand(image);
  const second = new OwnedStand(image);
  assert.notEqual(first.project, second.project);
});

test('failed log capture still removes owned resources and reports failure', async () => {
  const stand = new OwnedStand(image);
  stand.configured = true;
  const commands = [];
  stand.docker = async () => '';
  const artifacts = new Map();
  stand.evidence = {
    write: async (name, value) => {
      artifacts.set(name, value);
    },
  };
  stand.compose = async (arguments_) => {
    commands.push(arguments_[0]);
    if (arguments_[0] === 'logs') throw new Error('log capture failed');
    return '';
  };
  await assert.rejects(stand.close(), /log capture failed/);
  assert.deepEqual(commands, ['logs', 'ps', 'down']);
  assert.equal(JSON.parse(artifacts.get('cleanup.json')).errors.length, 1);
});

test('cleanup refuses a container whose ownership labels do not match', async () => {
  const stand = new OwnedStand(image);
  stand.configured = true;
  const commands = [];
  stand.evidence = { write: async () => {} };
  stand.compose = async (arguments_) => {
    commands.push(arguments_[0]);
    return arguments_[0] === 'ps' ? 'container-id' : '';
  };
  stand.docker = async () =>
    JSON.stringify([
      {
        Id: 'container-id',
        Config: { Labels: { synthetic: 'true', 'zns.fqa.run': 'another-run' } },
      },
    ]);
  await assert.rejects(stand.close(), /ownership changed/);
  assert.deepEqual(commands, ['logs', 'ps']);
});

function resourceStand(
  kind,
  { foreignLabel = false, foreignConsumer = false } = {},
) {
  const stand = new OwnedStand(image);
  stand.configured = true;
  const name = stand.project + '-resource';
  stand.resources = { [kind]: [name] };
  const commands = [];
  let isRemoved = false;
  stand.evidence = { write: async () => {} };
  stand.compose = async (arguments_) => {
    commands.push(arguments_[0]);
    if (arguments_[0] === 'down') isRemoved = true;
    return '';
  };
  stand.docker = async (arguments_) => {
    const [command, action] = arguments_;
    if (command === kind && action === 'ls') return isRemoved ? '' : name;
    if (command === kind && action === 'inspect')
      return JSON.stringify([
        {
          Labels: {
            synthetic: 'true',
            'zns.fqa.run': foreignLabel ? 'another-run' : stand.project,
            'zns.fqa.owner': 'fqa-owned',
            'com.docker.compose.project': stand.project,
          },
          Containers: {},
        },
      ]);
    if (command === 'ps') return foreignConsumer ? 'foreign-container' : '';
    throw new Error('Unexpected Docker command in cleanup test');
  };
  return { stand, commands };
}

for (const kind of ['network', 'volume']) {
  test(`partial startup checks ${kind} ownership without containers`, async () => {
    const { stand, commands } = resourceStand(kind, { foreignLabel: true });
    await assert.rejects(stand.close(), /ownership changed/);
    assert.deepEqual(commands, ['logs', 'ps']);
  });
  test(`cleanup refuses ${kind} used by a foreign container`, async () => {
    const { stand, commands } = resourceStand(kind, { foreignConsumer: true });
    await assert.rejects(stand.close(), /foreign consumers/);
    assert.deepEqual(commands, ['logs', 'ps']);
  });
  test(`partial startup removes an owned unused ${kind}`, async () => {
    const { stand, commands } = resourceStand(kind);
    await stand.close();
    assert.deepEqual(commands, ['logs', 'ps', 'down']);
  });
}

test('repeated signals during teardown remain handled until resource removal finishes', async () => {
  const stand = new OwnedStand(image);
  stand.evidence = { init: async () => {}, write: async () => {} };
  stand.docker = async () => {
    throw new Error('controlled startup failure');
  };
  await assert.rejects(stand.start(), /controlled startup failure/);
  stand.configured = true;
  stand.docker = async () => '';
  const commands = [];
  stand.compose = async (arguments_, options) => {
    commands.push(arguments_[0]);
    assert.equal(options.cleanup, true);
    if (['logs', 'down'].includes(arguments_[0])) {
      for (const signal of ['SIGINT', 'SIGINT', 'SIGTERM', 'SIGTERM']) {
        assert.ok(process.listeners(signal).includes(stand.interrupt));
        process.emit(signal);
        assert.ok(process.listeners(signal).includes(stand.interrupt));
      }
      await Promise.resolve();
    }
    return '';
  };
  await stand.close();
  assert.deepEqual(commands, ['logs', 'ps', 'down']);
  assert.equal(stand.signal.aborted, true);
  assert.equal(process.listeners('SIGINT').includes(stand.interrupt), false);
  assert.equal(process.listeners('SIGTERM').includes(stand.interrupt), false);
});

test('cancellation joins a real child before returning to resource cleanup', async (t) => {
  const evidence = new Evidence('child-join-' + randomUUID());
  await evidence.init();
  const controller = new AbortController();
  const command = executeJoined(
    process.execPath,
    [
      '-e',
      "process.on('SIGTERM',()=>{});require('node:fs').writeFileSync(process.argv[1],String(process.pid));setInterval(()=>{},1000)",
      evidence.file('pid.txt'),
    ],
    { signal: controller.signal, encoding: 'utf8', timeout: 10_000 },
  );
  const settled = Promise.allSettled([command]);
  t.after(async () => {
    controller.abort();
    await settled;
    if (pid) {
      try {
        process.kill(pid, 'SIGKILL');
      } catch (error) {
        if (error.code !== 'ESRCH') throw error;
      }
    }
  });
  let pid;
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      pid = Number(await evidence.read('pid.txt'));
      break;
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    await delay(20);
  }
  assert.ok(pid, 'Child must report readiness before cancellation');
  const reason = new Error('cancel observed ready child');
  controller.abort(reason);
  const [result] = await settled;
  assert.equal(result.status, 'rejected');
  assert.equal(result.reason, reason);
  assert.throws(
    () => process.kill(pid, 0),
    { code: 'ESRCH' },
    'Returned only after the child exited',
  );
});

test('already-aborted commands never spawn a child', () => {
  const controller = new AbortController();
  const reason = new Error('already cancelled');
  controller.abort(reason);
  assert.throws(
    () =>
      executeJoined(process.execPath, ['-e', 'process.exit(0)'], {
        signal: controller.signal,
      }),
    (error) => error === reason,
  );
});

function fakeProcess() {
  return Object.assign(new childProcess.ChildProcess(), {
    pid: 4242,
    unref() {},
    kill() {},
    stdout: { destroy() {} },
    stderr: { destroy() {} },
  });
}

test('browser cancellation before setup returns preserves independent artifacts', async (t) => {
  const artifacts = new Map();
  const controller = new AbortController();
  const navigationStarted = Promise.withResolvers();
  const navigation = Promise.withResolvers();
  let pageError;
  let closes = 0;
  let traces = 0;
  const page = {
    on: (_event, listener) => {
      pageError = listener;
    },
    goto: async () => {
      pageError(new Error('initial page error'));
      navigationStarted.resolve();
      return navigation.promise;
    },
    screenshot: async () => {
      throw new Error('screenshot unavailable');
    },
  };
  const context = {
    setDefaultTimeout: (value) => assert.equal(value, 15_000),
    setDefaultNavigationTimeout: (value) => assert.equal(value, 15_000),
    tracing: {
      start: async () => {},
      stop: async () => {
        traces++;
      },
    },
    newPage: async () => page,
  };
  const target = fakeProcess();
  t.mock.method(
    childProcess,
    'execFile',
    (_file, _arguments, _options, callback) => {
      queueMicrotask(() => {
        closes++;
        target.emit('close');
        navigation.reject(new Error('browser closed'));
        callback();
      });
      return fakeProcess();
    },
  );
  t.mock.method(chromium, 'launchServer', async (options) => {
    assert.equal(options.handleSIGINT, false);
    assert.equal(options.handleSIGTERM, false);
    assert.equal(options.handleSIGHUP, false);
    return { wsEndpoint: () => 'ws://127.0.0.1:12345', process: () => target };
  });
  t.mock.method(chromium, 'connect', async () => ({
    newContext: async () => context,
  }));
  const run = {
    client: { ui: 'http://127.0.0.1:12345' },
    artifact: (name) => name,
    save: async (name, value) => {
      artifacts.set(name, value);
    },
  };
  const opening = openTelegram(run, {
    signal: controller.signal,
    label: 'cancelled',
  });
  await navigationStarted.promise;
  const reason = new Error('cancel during navigation');
  controller.abort(reason);
  await assert.rejects(opening, (error) => error.errors.includes(reason));
  assert.deepEqual(artifacts.get('cancelled-browser-errors.json'), [
    'Error: initial page error',
  ]);
  assert.equal(traces, 1, 'Trace attempted despite screenshot failure');
  assert.equal(closes, 1, 'Cancellation and setup failure share teardown');
});

test('already-aborted browser setup does not launch', async (t) => {
  const launch = t.mock.method(chromium, 'launchServer', async () => {
    throw new Error('Unexpected launch');
  });
  const controller = new AbortController();
  const reason = new Error('already cancelled');
  controller.abort(reason);
  await assert.rejects(
    openTelegram({}, { signal: controller.signal }),
    (error) => error === reason,
  );
  assert.equal(launch.mock.callCount(), 0);
});

test('cancellation terminates a real parent and its descendant before cleanup', async (t) => {
  const evidence = new Evidence('tree-join-' + randomUUID());
  await evidence.init();
  const controller = new AbortController();
  const descendant =
    "process.on('SIGTERM',()=>{});process.send(process.pid);setInterval(()=>{},1000)";
  const parent =
    "const child=require('node:child_process').spawn(process.execPath,['-e',process.argv[2]],{stdio:['ignore','inherit','inherit','ipc']});child.on('message',pid=>require('node:fs').writeFileSync(process.argv[1],JSON.stringify([process.pid,pid])));setInterval(()=>{},1000)";
  const command = executeJoined(
    process.execPath,
    ['-e', parent, evidence.file('pids.json'), descendant],
    { signal: controller.signal, timeout: 10_000 },
  );
  const settled = Promise.allSettled([command]);
  t.after(async () => {
    controller.abort();
    await settled;
    const ownedPids = pids ?? [];
    for (const pid of ownedPids) {
      try {
        process.kill(pid, 'SIGKILL');
      } catch (error) {
        if (error.code !== 'ESRCH') throw error;
      }
    }
  });
  let pids;
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      pids = JSON.parse(await evidence.read('pids.json'));
      break;
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    await delay(20);
  }
  assert.equal(pids?.length, 2, 'Both processes must report readiness');
  const reason = new Error('cancel ready process tree');
  controller.abort(reason);
  const [result] = await settled;
  assert.equal(result.status, 'rejected');
  assert.equal(result.reason, reason);
  for (const pid of pids)
    assert.throws(() => process.kill(pid, 0), { code: 'ESRCH' });
});

test('setup timeout joins asynchronous owned tree termination', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const context = Promise.withResolvers();
  const target = fakeProcess();
  let completeKill;
  t.mock.method(
    childProcess,
    'execFile',
    (_file, _arguments, _options, callback) => {
      completeKill = () => {
        target.emit('close');
        callback();
        context.reject(new Error('killed'));
      };
      return fakeProcess();
    },
  );
  t.mock.method(chromium, 'launchServer', async () => ({
    wsEndpoint: () => 'ws://127.0.0.1:12345',
    process: () => target,
  }));
  const started = Promise.withResolvers();
  t.mock.method(chromium, 'connect', async () => ({
    newContext: async () => {
      started.resolve();
      return context.promise;
    },
  }));
  const opening = openTelegram({ save: async () => {} });
  const settled = Promise.allSettled([opening]);
  let hasSettled = false;
  void (async () => {
    await settled;
    hasSettled = true;
  })();
  await started.promise;
  t.mock.timers.tick(10_000);
  await Promise.resolve();
  assert.equal(typeof completeKill, 'function');
  assert.equal(hasSettled, false);
  completeKill();
  const [result] = await settled;
  assert.equal(result.status, 'rejected');
});

test('never-settling tree kill fails within its own deadline with retained ownership', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const target = fakeProcess();
  const killer = fakeProcess();
  t.mock.method(process, 'kill', () => {});
  const forceKill = t.mock.method(killer, 'kill');
  t.mock.method(childProcess, 'execFile', () => killer);
  const ending = terminateOwnedTree(target);
  const settled = Promise.allSettled([ending]);
  t.mock.timers.tick(5000);
  const [result] = await settled;
  assert.equal(result.status, 'rejected');
  assert.equal(result.reason.unjoined, true);
  assert.equal(result.reason.ownedPid, target.pid);
  assert.equal(
    forceKill.mock.callCount(),
    process.platform === 'win32' ? 1 : 0,
  );
  assert.equal(target.listenerCount('close'), 0);
});

test('failed browser termination still saves ownership and permits independent stack cleanup', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const target = fakeProcess();
  t.mock.method(process, 'kill', () => {});
  t.mock.method(childProcess, 'execFile', () => fakeProcess());
  t.mock.method(chromium, 'launchServer', async () => ({
    wsEndpoint: () => 'ws://127.0.0.1:12345',
    process: () => target,
  }));
  const started = Promise.withResolvers();
  const pendingContext = Promise.withResolvers();
  t.mock.method(chromium, 'connect', async () => ({
    newContext: async () => {
      started.resolve();
      return pendingContext.promise;
    },
  }));
  const artifacts = new Map();
  const run = {
    save: async (name, value) => {
      artifacts.set(name, value);
    },
  };
  const opening = openTelegram(run, { label: 'unjoined' });
  const settled = Promise.allSettled([opening]);
  await started.promise;
  t.mock.timers.tick(10_000);
  await Promise.resolve();
  t.mock.timers.tick(5000);
  const [result] = await settled;
  assert.equal(result.status, 'rejected');
  assert.deepEqual(artifacts.get('unjoined-process.json'), {
    pid: target.pid,
    joined: false,
  });
  assert.deepEqual(artifacts.get('unjoined-browser-errors.json'), []);
  const stand = new OwnedStand(image);
  stand.configured = true;
  stand.evidence = { write: async () => {} };
  stand.docker = async () => '';
  const commands = [];
  stand.compose = async (arguments_) => {
    commands.push(arguments_[0]);
    return '';
  };
  await stand.close();
  assert.ok(commands.includes('down'));
});

test('never-ready import with a real descendant times out, joins, then cleans the independent stand', async (t) => {
  const evidence = new Evidence('startup-hang-' + randomUUID());
  await evidence.init();
  const descendant = 'process.send(process.pid);setInterval(()=>{},1000)';
  const source =
    "const child=require('node:child_process').spawn(process.execPath,['-e',process.argv[2]],{stdio:['ignore','inherit','inherit','ipc']});child.on('message',async pid=>{require('node:fs').writeFileSync(process.argv[1],JSON.stringify([process.pid,pid]));console.log('startup entered');await import('data:text/javascript,while(true){}')});";
  const { stand, commands } = resourceStand('volume');
  let pids;
  t.after(() => {
    const ownedPids = pids ?? [];
    for (const pid of ownedPids) {
      try {
        process.kill(pid, 'SIGKILL');
      } catch (error) {
        if (error.code !== 'ESRCH') throw error;
      }
    }
  });
  const started = Date.now();
  let failure;
  try {
    await executeJoined(
      process.execPath,
      ['-e', source, evidence.file('pids.json'), descendant],
      { timeout: 2000, encoding: 'utf8' },
    );
  } catch (error) {
    failure = error;
  } finally {
    pids = JSON.parse(await evidence.read('pids.json'));
    for (const pid of pids)
      assert.throws(
        () => process.kill(pid, 0),
        { code: 'ESRCH' },
        'Tree joined before stand cleanup',
      );
    await stand.close();
  }
  assert.match(failure.message, /Command deadline exceeded/);
  assert.match(failure.output.stdout, /startup entered/);
  assert.ok(
    Date.now() - started < 9000,
    'Deadline plus termination allowance is bounded',
  );
  assert.deepEqual(commands, ['logs', 'ps', 'down']);
});

test('unjoined log capture preserves ownership and prevents destructive cleanup', async () => {
  const stand = new OwnedStand(image);
  stand.configured = true;
  const error = Object.assign(
    new Error('log process termination unconfirmed'),
    { unjoined: true, ownedPid: 4242 },
  );
  const artifacts = new Map();
  const commands = [];
  stand.evidence = {
    write: async (name, value) => {
      artifacts.set(name, value);
    },
  };
  stand.compose = async (arguments_) => {
    commands.push(arguments_[0]);
    stand.unjoinedChild = error;
    throw error;
  };
  await assert.rejects(stand.close(), (failure) => {
    assert.deepEqual(failure.errors, [error]);
    return true;
  });
  assert.deepEqual(commands, ['logs']);
  assert.deepEqual(JSON.parse(artifacts.get('cleanup.json')).errors, [
    { message: error.message, unjoined: true, ownedPid: 4242 },
  ]);
});

test('real output overflow joins the descendant tree before independent cleanup', async (t) => {
  const evidence = new Evidence('output-overflow-' + randomUUID());
  await evidence.init();
  const descendant = 'process.send(process.pid);setInterval(()=>{},1000)';
  const source =
    "const child=require('node:child_process').spawn(process.execPath,['-e',process.argv[2]],{stdio:['ignore','inherit','inherit','ipc']});child.on('message',pid=>{require('node:fs').writeFileSync(process.argv[1],JSON.stringify([process.pid,pid]));process.stdout.write(Buffer.alloc(8192,65));process.stderr.write(Buffer.alloc(8192,66))});setInterval(()=>{},1000)";
  const { stand, commands } = resourceStand('volume');
  let pids;
  t.after(() => {
    const ownedPids = pids ?? [];
    for (const pid of ownedPids) {
      try {
        process.kill(pid, 'SIGKILL');
      } catch (error) {
        if (error.code !== 'ESRCH') throw error;
      }
    }
  });
  let failure;
  try {
    await executeJoined(
      process.execPath,
      ['-e', source, evidence.file('pids.json'), descendant],
      { maxBuffer: 4096, timeout: 10_000 },
    );
  } catch (error) {
    failure = error;
  } finally {
    pids = JSON.parse(await evidence.read('pids.json'));
    for (const pid of pids)
      assert.throws(
        () => process.kill(pid, 0),
        { code: 'ESRCH' },
        'Overflow tree joined before stack cleanup',
      );
    await stand.close();
  }
  assert.equal(failure.code, 'ERR_CHILD_PROCESS_STDIO_MAXBUFFER');
  assert.equal(
    Buffer.byteLength(failure.output.stdout) +
      Buffer.byteLength(failure.output.stderr),
    4096,
  );
  assert.deepEqual(commands, ['logs', 'ps', 'down']);
});

test('joined spawn preserves successful output and rejects nonzero exit', async () => {
  assert.deepEqual(
    await executeJoined(process.execPath, [
      '-e',
      "process.stdout.write('out');process.stderr.write('err')",
    ]),
    { stdout: 'out', stderr: 'err' },
  );
  await assert.rejects(
    executeJoined(process.execPath, [
      '-e',
      "process.stderr.write('failed');process.exitCode=7",
    ]),
    (error) => {
      assert.equal(error.code, 7);
      assert.equal(error.output.stderr, 'failed');
      assert.equal(error.unjoined, true);
      assert.ok(
        error.ownedPid,
        'Even harmless abnormal exit retains uncertain ownership',
      );
      return true;
    },
  );
});

test('overflow with failed tree termination reports bounded output and retained ownership', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const target = fakeProcess();
  target.stdout = new PassThrough();
  target.stderr = new PassThrough();
  t.mock.method(childProcess, 'spawn', () => target);
  t.mock.method(childProcess, 'execFile', () => fakeProcess());
  t.mock.method(process, 'kill', () => {});
  const command = executeJoined('owned-command', [], { maxBuffer: 16 });
  const settled = Promise.allSettled([command]);
  target.stdout.write(Buffer.alloc(32, 65));
  t.mock.timers.tick(5000);
  const [result] = await settled;
  assert.equal(result.status, 'rejected');
  assert.equal(result.reason.unjoined, true);
  assert.equal(result.reason.ownedPid, target.pid);
  assert.equal(result.reason.cause.code, 'ERR_CHILD_PROCESS_STDIO_MAXBUFFER');
  assert.equal(Buffer.byteLength(result.reason.output.stdout), 16);
});

test('unjoined browser phase blocks stand cleanup even when phase evidence also fails', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const target = fakeProcess();
  target.stdout = new PassThrough();
  target.stderr = new PassThrough();
  t.mock.method(childProcess, 'spawn', () => target);
  t.mock.method(childProcess, 'execFile', () => fakeProcess());
  t.mock.method(process, 'kill', () => {});
  const stand = new OwnedStand(image);
  stand.configured = true;
  const commands = [];
  const artifacts = new Map();
  stand.compose = async (arguments_) => {
    commands.push(arguments_[0]);
    return '';
  };
  stand.evidence = {
    write: async (name, value) => {
      artifacts.set(name, value);
    },
  };
  const evidenceFailure = new Error('phase evidence unavailable');
  let attemptedEvidence;
  const run = {
    save: async (_name, value) => {
      assert.equal(
        stand.unjoinedChild.unjoined,
        true,
        'Ownership retained before evidence operation',
      );
      assert.equal(stand.unjoinedChild.ownedPid, target.pid);
      attemptedEvidence = value;
      throw evidenceFailure;
    },
  };
  const phase = runPhase(stand, run, 'phase-test', 'before');
  const settled = Promise.allSettled([phase]);
  t.mock.timers.tick(60_000);
  t.mock.timers.tick(5000);
  const [result] = await settled;
  assert.equal(result.status, 'rejected');
  assert.deepEqual(result.reason.errors, [
    stand.unjoinedChild,
    evidenceFailure,
  ]);
  assert.equal(attemptedEvidence.error.unjoined, true);
  assert.equal(attemptedEvidence.error.ownedPid, target.pid);
  await assert.rejects(stand.close(), (error) => {
    assert.deepEqual(error.errors, [stand.unjoinedChild]);
    return true;
  });
  assert.deepEqual(
    commands,
    [],
    'No logs, inspection or removal while phase remains unjoined',
  );
  const cleanup = JSON.parse(artifacts.get('cleanup.json'));
  assert.equal(cleanup.errors[0].unjoined, true);
  assert.equal(cleanup.errors[0].ownedPid, target.pid);
});

test('unexpected root exit retains a live descendant and prevents stand teardown', async (t) => {
  const evidence = new Evidence('root-exit-' + randomUUID());
  await evidence.init();
  const descendant = 'process.send(process.pid);setInterval(()=>{},1000)';
  const source =
    "const child=require('node:child_process').spawn(process.execPath,['-e',process.argv[2]],{detached:true,windowsHide:true,stdio:['ignore','ignore','ignore','ipc']});child.on('message',pid=>{require('node:fs').writeFileSync(process.argv[1],JSON.stringify([process.pid,pid]));child.disconnect();process.exit(7)})";
  const spawn = childProcess.spawn;
  t.mock.method(childProcess, 'spawn', (_file, _arguments, options) =>
    spawn(
      process.execPath,
      ['-e', source, evidence.file('pids.json'), descendant],
      options,
    ),
  );
  t.after(async () => {
    const ownedPids = JSON.parse(await evidence.read('pids.json'));
    for (const pid of ownedPids) {
      try {
        process.kill(pid, 'SIGKILL');
      } catch (error) {
        if (error.code !== 'ESRCH') throw error;
      }
      let hasExited = false;
      for (let attempt = 0; !hasExited && attempt < 100; attempt++) {
        try {
          process.kill(pid, 0);
        } catch (error) {
          if (error.code !== 'ESRCH') throw error;
          hasExited = true;
        }
        await delay(20);
      }
      assert.ok(hasExited, 'Explicit test-owned PID cleanup completed');
    }
  });
  const stand = new OwnedStand(image);
  stand.configured = true;
  const commands = [];
  const artifacts = new Map();
  stand.compose = async (arguments_) => {
    commands.push(arguments_[0]);
    return '';
  };
  stand.evidence = {
    write: async (name, value) => {
      artifacts.set(name, value);
    },
  };
  const run = {
    save: async (name, value) => {
      artifacts.set(name, value);
    },
  };
  await assert.rejects(
    runPhase(stand, run, 'root-exit-test', 'before'),
    (error) => {
      assert.equal(error.code, 7);
      assert.equal(error.unjoined, true);
      assert.equal(error, stand.unjoinedChild);
      return true;
    },
  );
  const pids = JSON.parse(await evidence.read('pids.json'));
  assert.throws(() => process.kill(pids[0], 0), { code: 'ESRCH' });
  assert.doesNotThrow(
    () => process.kill(pids[1], 0),
    'Independent descendant still exists',
  );
  assert.equal(stand.unjoinedChild.ownedPid, pids[0]);
  assert.equal(artifacts.get('before-process.json').error.ownedPid, pids[0]);
  await assert.rejects(stand.close(), (error) =>
    error.errors.includes(stand.unjoinedChild),
  );
  assert.deepEqual(commands, []);
  assert.equal(
    JSON.parse(artifacts.get('cleanup.json')).errors[0].unjoined,
    true,
  );
});

test('pre-spawn failure without a PID does not claim an unjoined tree', async () => {
  await assert.rejects(
    executeJoined('missing-fqa-command-' + randomUUID(), []),
    (error) => {
      assert.equal(error.code, 'ENOENT');
      assert.equal(error.unjoined, undefined);
      assert.equal(error.ownedPid, undefined);
      return true;
    },
  );
});

test('restart resolves current dynamic endpoints only after services are ready', async () => {
  const stand = new OwnedStand(image);
  const commands = [];
  let isReady = false;
  stand.compose = async (arguments_) => {
    commands.push(arguments_);
    if (arguments_[0] === 'up') isReady = true;
    else if (arguments_[0] === 'port') {
      assert.equal(isReady, true);
      return arguments_[1] === 'fake' ? '127.0.0.1:32939' : '127.0.0.1:32938';
    }
    return '';
  };
  assert.deepEqual(await stand.restart(), {
    ui: 'http://127.0.0.1:32939',
    api: 'http://127.0.0.1:32938',
  });
  assert.deepEqual(commands[0], ['restart', 'api', 'bot', 'fake']);
  assert.equal(commands[1].includes('--wait'), true);
  assert.deepEqual(commands.slice(2), [
    ['port', 'fake', '8080'],
    ['port', 'api', '8080'],
  ]);
});

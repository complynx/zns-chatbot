// Optional local-only synthetic stand. No Codex credentials enter Docker.
import { spawn, spawnSync } from 'node:child_process';
import { mkdir } from 'node:fs/promises';
import net from 'node:net';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';

const root = fileURLToPath(new URL('..', import.meta.url));
const binary = path.join(
  root,
  'tools.local',
  process.platform === 'win32' ? 'zns-codex.exe' : 'zns-codex',
);
const owned = new Set();
const state = { isStopping: false, hasFailure: false };
const common = {
  ...process.env,
  ZNS_ENV: 'sandbox',
  ZNS_PARENT_STDIN: 'false',
  BIND_HOST: '127.0.0.1',
  SANDBOX_SIGNING_KEY: 'sandbox-only-do-not-use-in-production-123456789',
  TELEGRAM_TOKEN: 'sandbox',
};

function start(name, command, arguments_, environment) {
  if (state.isStopping) throw new Error('Launcher is stopping');
  const child = spawn(command, arguments_, {
    cwd: root,
    env: environment,
    shell: false,
    windowsHide: true,
    detached: process.platform !== 'win32',
    stdio: ['pipe', 'ignore', 'ignore'],
  });
  owned.add(child);
  child.on('error', () => {
    state.hasFailure = true;
    owned.delete(child);
    console.error(`${name} could not start`);
  });
  child.on('exit', () => owned.delete(child));
  return child;
}

async function waitExit(child, timeout) {
  const deadline = Date.now() + timeout;
  while (
    child.exitCode === null &&
    child.signalCode === null &&
    owned.has(child)
  ) {
    if (Date.now() >= deadline) return false;
    await delay(100);
  }
  return true;
}

async function forceStop(child) {
  if (child.exitCode !== null || child.signalCode !== null) return true;
  if (process.platform === 'win32') {
    spawnSync('taskkill.exe', ['/PID', String(child.pid), '/T', '/F'], {
      shell: false,
      windowsHide: true,
      stdio: 'ignore',
      timeout: 5000,
    });
  } else {
    try {
      process.kill(-child.pid, 'SIGKILL');
    } catch (error) {
      if (error.code !== 'ESRCH') state.hasFailure = true;
    }
  }
  if (await waitExit(child, 5000)) return true;
  state.hasFailure = true;
  console.error('An owned process did not exit after forced termination');
  child.stdin.destroy();
  child.unref();
  return false;
}

async function command(
  name,
  executable,
  arguments_,
  timeout,
  environment = common,
) {
  const child = start(name, executable, arguments_, environment);
  child.stdin.end();
  if (!(await waitExit(child, timeout))) {
    await forceStop(child);
    throw new Error(`${name} timed out`);
  }
  if (child.exitCode !== 0) throw new Error(`${name} failed`);
}

async function freePort(port) {
  const listener = net.createServer();
  await new Promise((resolve, reject) => {
    listener.once('error', reject);
    listener.listen(port, '127.0.0.1', resolve);
  });
  await new Promise((resolve, reject) =>
    listener.close((error) => (error ? reject(error) : resolve())),
  );
}

async function ready(child, port) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline && owned.has(child) && !state.isStopping) {
    try {
      const response = await fetch(`http://127.0.0.1:${port}/healthz`, {
        signal: AbortSignal.timeout(2000),
      });
      if (response.ok) return;
    } catch {
      /*
      Readiness retries are bounded by the deadline.
      */
    }
    await delay(200);
  }
  throw new Error('Native service did not become ready');
}

async function service(name, port, environment) {
  const child = start(name, binary, [name], {
    ...common,
    ...environment,
    PORT: String(port),
    ZNS_PARENT_STDIN: 'true',
  });
  await ready(child, port);
  child.on('exit', () => {
    if (state.isStopping) {
      return;
    }

    state.hasFailure = true;
    console.error(`${name} stopped unexpectedly`);
    void stop();
  });
}

async function stop() {
  if (state.isStopping) return;
  state.isStopping = true;
  process.stdin.destroy();
  const children = [...owned];
  for (const child of children) child.stdin.end();
  await Promise.all(
    children.map(async (child) => {
      if (await waitExit(child, 7000)) {
        return;
      }

      await forceStop(child);
    }),
  );
  process.exitCode = state.hasFailure ? 1 : 0;
}

process.on('SIGINT', () => {
  void stop();
});
process.on('SIGTERM', () => {
  void stop();
});
process.stdin.setEncoding('utf8');
process.stdin.on('data', (text) => {
  if (text.trim() === 'stop') void stop();
});

try {
  if (process.env.SYNTHETIC_ONLY !== 'true')
    throw new Error('Set SYNTHETIC_ONLY=true explicitly');
  const executable = process.env.CODEX_EXECUTABLE;
  if (!executable || !path.isAbsolute(executable)) {
    throw new Error(
      'CODEX_EXECUTABLE must name an absolute installed Codex executable',
    );
  }
  for (const port of [8093, 8094, 8095]) await freePort(port);
  process.chdir(root);
  await mkdir('tools.local', { recursive: true });
  await command(
    'Go build',
    'go',
    ['build', '-o', binary, './cmd/zns'],
    120_000,
  );
  await command(
    'Isolated PostgreSQL',
    'docker',
    [
      'compose',
      '--project-name',
      'zns-codex-sandbox',
      '-f',
      'compose.codex.yaml',
      'up',
      '-d',
      '--wait',
      '--wait-timeout',
      '60',
    ],
    90_000,
  );
  await command('Migration', binary, ['migrate'], 30_000, {
    ...common,
    DATABASE_URL:
      'postgres://postgres:sandbox-owner-only@127.0.0.1:55433/zns?sslmode=disable',
  });
  await service('api', 8094, {
    DATABASE_URL:
      'postgres://zns_api:sandbox-api-only@127.0.0.1:55433/zns?sslmode=disable',
  });
  await service('fake', 8093, {
    DATABASE_URL:
      'postgres://zns_bot:sandbox-bot-only@127.0.0.1:55433/zns?sslmode=disable',
    MINIAPP_URL: 'http://127.0.0.1:8095',
  });
  await service('bot', 8095, {
    DATABASE_URL:
      'postgres://zns_bot:sandbox-bot-only@127.0.0.1:55433/zns?sslmode=disable',
    CORE_URL: 'http://127.0.0.1:8094',
    TELEGRAM_BASE_URL: 'http://127.0.0.1:8093',
    WEBAPP_URL: 'http://127.0.0.1:8093/miniapp/',
    MODEL_PROVIDER: 'codex',
  });
  console.log(
    'Synthetic Codex stand ready: http://127.0.0.1:8093 (gpt-6-luna). Type stop or press Ctrl+C to stop native processes; isolated PostgreSQL persists.',
  );
} catch (error) {
  state.hasFailure = true;
  console.error(error.message);
  await stop();
}

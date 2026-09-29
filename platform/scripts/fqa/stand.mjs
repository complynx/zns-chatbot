import childProcess from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { Evidence } from './evidence.mjs';

const root = fileURLToPath(new URL('../..', import.meta.url));
const imagePattern = /^sha256:[a-f\d]{64}$/;

// One owned tree boundary for Docker CLI plugins and browser processes.
export function terminateOwnedTree(child) {
  return new Promise((resolve, reject) => {
    let killer;
    let hasClosed = false;
    let hasKilled = false;
    let hasFinished = false;
    const finish = (cause) => {
      if (hasFinished || (!cause && !(hasClosed && hasKilled))) return;
      hasFinished = true;
      clearTimeout(deadline);
      child.removeListener('close', closed);
      if (cause) {
        const error = new Error(
          `Owned process tree ${child.pid} termination unconfirmed`,
          { cause },
        );
        error.unjoined = true;
        error.ownedPid = child.pid;
        killer?.kill('SIGKILL');
        for (const process_ of [child, killer]) {
          process_?.stdout?.destroy();
          process_?.stderr?.destroy();
          process_?.unref();
        }
        reject(error);
      } else resolve();
    };
    const closed = () => {
      hasClosed = true;
      finish();
    };
    const deadline = setTimeout(
      () => finish(new Error('Tree termination deadline exceeded')),
      5000,
    );
    child.once('close', closed);
    if (!child.pid || child.exitCode !== null || child.signalCode !== null) {
      finish(new Error('Owned live process handle is unavailable'));
      return;
    }
    if (process.platform === 'win32') {
      killer = childProcess.execFile(
        'taskkill',
        ['/PID', String(child.pid), '/T', '/F'],
        { windowsHide: true, timeout: 4000, killSignal: 'SIGKILL' },
        (error) => {
          if (error) finish(error);
          else {
            hasKilled = true;
            finish();
          }
        },
      );
    } else {
      try {
        process.kill(-child.pid, 'SIGKILL');
        hasKilled = true;
        finish();
      } catch (error) {
        finish(error);
      }
    }
  });
}

// Trusted commands may exit zero only after their required descendants finish.
// Forced termination joins the owned tree; abnormal root exit retains uncertainty.
export function executeJoined(
  file,
  arguments_,
  {
    signal,
    timeout = 120_000,
    maxBuffer = 1024 * 1024,
    encoding = 'utf8',
    ...options
  } = {},
) {
  signal?.throwIfAborted();
  if (!Number.isSafeInteger(maxBuffer) || maxBuffer < 1)
    throw new Error('Command output limit must be a positive byte count');
  return new Promise((resolve, reject) => {
    let failure;
    let termination;
    let hasTerminated = false;
    let hasClosed = false;
    let hasFinished = false;
    let bytes = 0;
    const stdout = [];
    const stderr = [];
    const child = childProcess.spawn(file, arguments_, {
      ...options,
      detached: process.platform !== 'win32',
      windowsHide: true,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const settle = (error) => {
      if (hasFinished) return;
      hasFinished = true;
      clearTimeout(deadline);
      signal?.removeEventListener('abort', abort);
      const output = {
        stdout: Buffer.concat(stdout).toString(encoding),
        stderr: Buffer.concat(stderr).toString(encoding),
      };
      if (error) {
        error.output = output;
        reject(error);
      } else resolve(output);
    };
    const complete = () => {
      if (hasClosed && (!termination || hasTerminated)) settle(failure);
    };
    const terminate = (reason) => {
      if (termination || hasFinished) return;
      failure ||= reason;
      termination = (async () => {
        try {
          await terminateOwnedTree(child);
          hasTerminated = true;
          complete();
        } catch (error) {
          settle(
            Object.assign(
              new AggregateError(
                [failure, error],
                'Command failed and tree termination is unconfirmed',
                { cause: failure },
              ),
              { unjoined: true, ownedPid: error.ownedPid },
            ),
          );
        }
      })();
    };
    const collect = (chunks, chunk) => {
      const remaining = maxBuffer - bytes;
      if (remaining > 0) {
        const kept = chunk.subarray(0, remaining);
        chunks.push(Buffer.from(kept));
        bytes += kept.length;
      }
      if (chunk.length <= remaining) return;
      const error = new Error('Command output limit exceeded');
      error.code = 'ERR_CHILD_PROCESS_STDIO_MAXBUFFER';
      terminate(error);
    };
    const abort = () => terminate(signal.reason);
    const deadline = setTimeout(
      () => terminate(new Error('Command deadline exceeded')),
      timeout,
    );
    child.stdout.on('data', (chunk) => collect(stdout, chunk));
    child.stderr.on('data', (chunk) => collect(stderr, chunk));
    signal?.addEventListener('abort', abort, { once: true });
    if (signal?.aborted) abort();
    child.once('error', (error) => {
      failure ||= error;
    });
    child.once('close', (code, signal_) => {
      hasClosed = true;
      if (code !== 0 || signal_) {
        failure ||= new Error(`Command exited with ${signal_ ?? code}`);
        failure.code ??= code;
        failure.signal ??= signal_;
        // Root close cannot prove that descendants with independent stdio exited.
        if (!termination && child.pid) {
          failure.unjoined = true;
          failure.ownedPid = child.pid;
        }
      }
      complete();
    });
  });
}

// Transform the existing sandbox composition; no independently maintained service graph.
export function isolatedConfig(source, { project, image, postgresImage }) {
  if (!/^synthetic-qa-zns-[a-f\d-]{36}$/.test(project))
    throw new Error('Invalid synthetic project');
  if ([image, postgresImage].some((value) => !imagePattern.test(value)))
    throw new Error('Local immutable image IDs are required');
  const config = structuredClone(source);
  config.name = project;
  const database = project.replaceAll('-', '_');
  for (const [name, service] of Object.entries(config.services)) {
    delete service.build;
    delete service.ports;
    service.container_name = `${project}-${name}`;
    service.restart = 'no';
    service.pull_policy = 'never';
    service.image = name === 'postgres' ? postgresImage : image;
    service.labels = {
      synthetic: 'true',
      'zns.fqa.run': project,
      'zns.fqa.owner': 'fqa-owned',
    };
    if (service.environment?.DATABASE_URL) {
      const url = new URL(service.environment.DATABASE_URL);
      url.pathname = '/' + database;
      service.environment.DATABASE_URL = url.href;
    }
    if (name === 'postgres') {
      service.environment.POSTGRES_DB = database;
      service.healthcheck.test = [
        'CMD-SHELL',
        `pg_isready -U postgres -d ${database}`,
      ];
    }
    if (['api', 'fake'].includes(name)) {
      service.ports = [
        { target: 8080, published: '0', host_ip: '127.0.0.1', protocol: 'tcp' },
      ];
    }
  }
  for (const [name, network] of Object.entries(config.networks)) {
    network.name = `${project}-${name}`;
    network.labels = {
      synthetic: 'true',
      'zns.fqa.run': project,
      'zns.fqa.owner': 'fqa-owned',
    };
  }
  for (const [name, volume] of Object.entries(config.volumes)) {
    volume.name = `${project}-${name}`;
    volume.labels = {
      synthetic: 'true',
      'zns.fqa.run': project,
      'zns.fqa.owner': 'fqa-owned',
    };
  }
  return config;
}

export class OwnedStand {
  configured = false;
  unjoinedChild = false;
  resources = { network: [], volume: [] };
  constructor(image) {
    if (!imagePattern.test(image ?? ''))
      throw new Error(
        'Set FQA_IMAGE to an existing frozen sandbox image ID (sha256:...)',
      );
    this.image = image;
    this.project = 'synthetic-qa-zns-' + randomUUID();
    this.evidence = new Evidence(this.project);
    this.controller = new AbortController();
    this.signal = this.controller.signal;
    this.interrupt = () => this.controller.abort(new Error('FQA interrupted'));
    this.context = process.env.FQA_DOCKER_CONTEXT;
  }

  async docker(arguments_, { cleanup = false } = {}) {
    let output;
    try {
      output = await executeJoined(
        'docker',
        [...(this.context ? ['--context', this.context] : []), ...arguments_],
        {
          cwd: root,
          encoding: 'utf8',
          timeout: 120_000,
          maxBuffer: 8 * 1024 * 1024,
          env: Object.fromEntries(
            Object.entries(process.env).filter(
              ([key]) =>
                !key.startsWith('ORDER_FIXTURE_') &&
                !key.startsWith('EXPORT_FIXTURE_') &&
                !key.startsWith('COMPOSE_'),
            ),
          ),
          ...(!cleanup && { signal: this.signal }),
        },
      );
    } catch (error) {
      if (error.unjoined) this.unjoinedChild = error;
      throw error;
    }
    return output.stdout.trim();
  }

  compose(arguments_, options) {
    return this.docker(
      [
        'compose',
        '--project-directory',
        root,
        '-p',
        this.project,
        '-f',
        this.evidence.file('compose.json'),
        ...arguments_,
      ],
      options,
    );
  }

  async start() {
    await this.evidence.init();
    process.on('SIGINT', this.interrupt);
    process.on('SIGTERM', this.interrupt);
    const started = Date.now();
    this.context ||= await this.docker(['context', 'show']);
    const daemon = JSON.parse(
      await this.docker([
        'context',
        'inspect',
        this.context,
        '--format',
        '{{json .Endpoints.docker.Host}}',
      ]),
    );
    if (!daemon.startsWith('npipe://') && !daemon.startsWith('unix://'))
      throw new Error('FQA requires a local Docker context');
    const image = await this.docker([
      'image',
      'inspect',
      this.image,
      '--format',
      '{{.Id}}',
    ]);
    const postgresImage = await this.docker([
      'image',
      'inspect',
      'postgres:17-alpine',
      '--format',
      '{{.Id}}',
    ]);
    const source = JSON.parse(
      await this.docker([
        'compose',
        '-f',
        'compose.yaml',
        '-f',
        'compose.qa.yaml',
        'config',
        '--format',
        'json',
      ]),
    );
    const config = isolatedConfig(source, {
      project: this.project,
      image,
      postgresImage,
    });
    await this.evidence.write(
      'compose.json',
      JSON.stringify(config, undefined, 2),
      'wx',
    );
    this.resources = {
      network: Object.values(config.networks).map(({ name }) => name),
      volume: Object.values(config.volumes).map(({ name }) => name),
    };
    this.configured = true;
    await this.evidence.write(
      'stand.json',
      JSON.stringify(
        {
          project: this.project,
          candidate: image,
          postgresImage,
          database: config.services.postgres.environment.POSTGRES_DB,
          source: ['compose.yaml', 'compose.qa.yaml'],
          purpose: 'Synthetic black-box regression; no stage acceptance',
          owner: 'fqa-owned',
        },
        undefined,
        2,
      ),
      'wx',
    );
    await this.compose([
      'up',
      '-d',
      '--wait',
      '--wait-timeout',
      '90',
      'postgres',
    ]);
    await this.compose(['run', '--rm', '--no-deps', 'migrate']);
    await this.compose([
      'run',
      '--rm',
      '--no-deps',
      '-e',
      'ORDER_FIXTURE_DEADLINE=2099-01-01T00:00:00Z',
      'migrate',
      'fixture',
    ]);
    await this.compose([
      'up',
      '-d',
      '--no-build',
      '--wait',
      '--wait-timeout',
      '90',
      'api',
      'model',
      'fake',
      'bot',
    ]);
    const ui = await this.address('fake');
    const api = await this.address('api');
    await this.evidence.write(
      'ready.json',
      JSON.stringify({ ui, api, setupMs: Date.now() - started }),
    );
    return { ui, api };
  }

  async address(service) {
    const address = await this.compose(['port', service, '8080']);
    if (!/^127\.0\.0\.1:\d+$/.test(address))
      throw new Error('Expected one loopback-only dynamic port');
    return 'http://' + address;
  }

  async restart() {
    await this.compose(['restart', 'api', 'bot', 'fake']);
    await this.compose([
      'up',
      '-d',
      '--no-build',
      '--wait',
      '--wait-timeout',
      '90',
      'api',
      'bot',
      'fake',
    ]);
    return { ui: await this.address('fake'), api: await this.address('api') };
  }

  owns(labels) {
    return (
      labels?.synthetic === 'true' &&
      labels['zns.fqa.run'] === this.project &&
      labels['zns.fqa.owner'] === 'fqa-owned' &&
      labels['com.docker.compose.project'] === this.project
    );
  }

  async verifyCleanupOwnership() {
    const containers = await this.compose(['ps', '--all', '--quiet'], {
      cleanup: true,
    });
    const owned = new Set();
    if (containers) {
      const records = JSON.parse(
        await this.docker(['inspect', ...containers.split(/\s+/)], {
          cleanup: true,
        }),
      );
      for (const record of records) {
        if (!this.owns(record.Config.Labels))
          throw new Error('Refusing cleanup: container ownership changed');
        owned.add(record.Id);
      }
    }
    // Inspect named resources even when startup failed before creating any container.
    for (const [kind, names] of Object.entries(this.resources)) {
      if (names.length === 0) continue;
      const listed = await this.docker([kind, 'ls', '--format', '{{.Name}}'], {
        cleanup: true,
      });
      const existing = new Set(listed.split('\n'));
      for (const name of names) {
        if (!existing.has(name)) continue;
        const [resource] = JSON.parse(
          await this.docker([kind, 'inspect', name], { cleanup: true }),
        );
        if (!this.owns(resource.Labels))
          throw new Error(`Refusing cleanup: ${kind} ownership changed`);
        const consumers = await this.docker(
          [
            'ps',
            '--all',
            '--no-trunc',
            '--filter',
            `${kind}=${name}`,
            '--format',
            '{{.ID}}',
          ],
          { cleanup: true },
        );
        const references = consumers ? consumers.split('\n') : [];
        if (kind === 'network')
          references.push(...Object.keys(resource.Containers ?? {}));
        if (references.some((id) => !owned.has(id)))
          throw new Error(`Refusing cleanup: ${kind} has foreign consumers`);
      }
    }
  }

  async verifyRemoved() {
    const remaining = await this.docker(
      [
        'ps',
        '--all',
        '--quiet',
        '--filter',
        `label=com.docker.compose.project=${this.project}`,
      ],
      { cleanup: true },
    );
    if (remaining) throw new Error('Owned containers remain after cleanup');
    for (const [kind, names] of Object.entries(this.resources)) {
      if (names.length === 0) continue;
      const listed = await this.docker([kind, 'ls', '--format', '{{.Name}}'], {
        cleanup: true,
      });
      const existing = new Set(listed.split('\n'));
      if (names.some((name) => existing.has(name)))
        throw new Error(`Owned ${kind} remains after cleanup`);
    }
  }

  async close() {
    try {
      if (!this.configured) return;
      const failures = [];
      if (this.unjoinedChild) failures.push(this.unjoinedChild);
      try {
        if (this.unjoinedChild) throw this.unjoinedChild;
        await this.evidence.write(
          'compose.log',
          await this.compose(['logs', '--no-color', '--timestamps'], {
            cleanup: true,
          }),
        );
      } catch (error) {
        if (!failures.includes(error)) failures.push(error);
      }
      try {
        // Log collection itself may have lost ownership of a Docker CLI tree.
        if (this.unjoinedChild) throw this.unjoinedChild;
        await this.verifyCleanupOwnership();
        await this.compose(['down', '--volumes', '--timeout', '10'], {
          cleanup: true,
        });
        await this.verifyRemoved();
      } catch (error) {
        if (!failures.includes(error)) failures.push(error);
      }
      try {
        await this.evidence.write(
          'cleanup.json',
          JSON.stringify(
            { project: this.project, errors: failures },
            (_key, value) =>
              value instanceof Error
                ? {
                    message: value.message,
                    ...(value.unjoined && {
                      unjoined: true,
                      ownedPid: value.ownedPid,
                    }),
                    ...(value.cause && { cause: value.cause }),
                    ...(value.errors && { errors: value.errors }),
                  }
                : value,
          ),
        );
      } catch (error) {
        failures.push(error);
      }
      if (failures.length > 0)
        throw new AggregateError(
          failures,
          'Stand cleanup/evidence failed: ' + failures.map(String).join('\n'),
        );
    } finally {
      // Repeated signals must not terminate teardown while Docker resources still exist.
      process.removeListener('SIGINT', this.interrupt);
      process.removeListener('SIGTERM', this.interrupt);
    }
  }
}

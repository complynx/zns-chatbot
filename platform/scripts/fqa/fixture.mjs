import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../..', import.meta.url));
const allowed = new Map([
  [
    'fixture',
    new Set([
      'ORDER_FIXTURE_DEADLINE',
      'ORDER_FIXTURE_ID',
      'ORDER_FIXTURE_AGE',
      'ORDER_FIXTURE_ADMIN_COUNTRY',
      'ORDER_FIXTURE_EXTRA',
      'ORDER_FIXTURE_REMAINING',
    ]),
  ],
  [
    'export-fixture',
    new Set([
      'EXPORT_FIXTURE_ORDER_ID',
      'EXPORT_FIXTURE_CHOICE',
      'EXPORT_FIXTURE_ADMIN_ENABLED',
      'EXPORT_FIXTURE_BATCH_TAG',
      'EXPORT_FIXTURE_BATCH_COUNT',
    ]),
  ],
]);

export function fixtureArguments(mode, environment) {
  const keys = Object.keys(environment);
  if (
    !allowed.has(mode) ||
    keys.length === 0 ||
    Object.entries(environment).some(
      ([key, value]) =>
        !allowed.get(mode).has(key) || typeof value !== 'string',
    )
  )
    throw new Error('Unknown fixture mode or environment variable');
  return [
    'run',
    '--rm',
    '--no-deps',
    ...keys.flatMap((key) => ['-e', key]),
    'migrate',
    mode,
  ];
}

export class Fixtures {
  constructor({
    context = process.platform === 'win32' ? 'desktop-linux' : 'default',
  } = {}) {
    this.context = context;
  }

  docker(arguments_, environment = {}) {
    // Remove inherited fixture settings. Each invocation must declare its full mutation.
    const environment_ = Object.fromEntries(
      Object.entries(process.env).filter(
        ([key]) =>
          !key.startsWith('ORDER_FIXTURE_') &&
          !key.startsWith('EXPORT_FIXTURE_'),
      ),
    );
    return execFileSync('docker', ['--context', this.context, ...arguments_], {
      cwd: root,
      env: { ...environment_, ...environment },
      encoding: 'utf8',
      timeout: 60_000,
      maxBuffer: 4 * 1024 * 1024,
    });
  }

  preflight() {
    return this.docker(['info', '--format', '{{.ServerVersion}}']);
  }

  apply(mode, environment) {
    return this.docker(
      [
        'compose',
        '-f',
        'compose.yaml',
        '-f',
        'compose.qa.yaml',
        ...fixtureArguments(mode, environment),
      ],
      environment,
    );
  }

  restart(services = ['bot', 'fake']) {
    if (
      services.length === 0 ||
      services.some(
        (service) => !['bot', 'fake', 'api', 'model'].includes(service),
      )
    )
      throw new Error('Unsupported sandbox service');
    return this.docker([
      'compose',
      '-f',
      'compose.yaml',
      '-f',
      'compose.qa.yaml',
      'restart',
      ...services,
    ]);
  }
}

// Full local gate: fail closed when the integration stand is unavailable.
import { spawnSync } from 'node:child_process';
import process from 'node:process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

if (!process.env.TEST_DATABASE_URL)
  throw new Error('TEST_DATABASE_URL is required for the full gate');
if (!process.env.SANDBOX_URL)
  throw new Error('SANDBOX_URL is required for the full gate');

const root = fileURLToPath(new URL('..', import.meta.url));
const migrateRoot = path.resolve(root, '../tools/migrate');
const isWindows = process.platform === 'win32';
// Include every authored package root without scanning installed dependencies.
const packages = [
  './cmd/...',
  './internal/...',
  './integration/...',
  './identityprovision/...',
  './deploy/...',
];
// Each integration case migrates its own database. Bound parallel migrations
// independently of host CPU count; concurrency tests still run their actors.
const testArguments = ['-race', '-count=1', '-p', '2', '-parallel', '4'];

function run(command, arguments_, cwd = root) {
  const result = spawnSync(command, arguments_, {
    cwd,
    stdio: 'inherit',
    shell: false,
    env: process.env,
  });
  if (result.error) throw result.error;
  if (result.status !== 0)
    throw new Error(`${command} failed: ${result.status}`);
}

const lintBinary = path.join(
  root,
  'tools.local',
  `golangci-lint${isWindows ? '.exe' : ''}`,
);
run('go', [
  '-C',
  'tools',
  'build',
  '-o',
  lintBinary,
  'github.com/golangci/golangci-lint/v2/cmd/golangci-lint',
]);
run('go', [
  '-C',
  'tools/sqlc',
  'tool',
  'sqlc',
  'diff',
  '-f',
  '../../sqlc.yaml',
]);
run(lintBinary, ['config', 'verify']);
run(lintBinary, ['run', ...packages]);
run(lintBinary, ['fmt', '--diff']);
run(
  lintBinary,
  ['run', '--config', path.join(root, '.golangci.yml'), './...'],
  migrateRoot,
);
run(
  lintBinary,
  ['fmt', '--diff', '--config', path.join(root, '.golangci.yml')],
  migrateRoot,
);
const vulnerabilityBinary = path.join(
  root,
  'tools.local',
  `govulncheck${isWindows ? '.exe' : ''}`,
);
run('go', [
  '-C',
  'tools',
  'build',
  '-o',
  vulnerabilityBinary,
  'golang.org/x/vuln/cmd/govulncheck',
]);
run(vulnerabilityBinary, packages);
run(vulnerabilityBinary, ['./...'], migrateRoot);
run(vulnerabilityBinary, [
  '-C',
  'tools/sqlc',
  'github.com/sqlc-dev/sqlc/cmd/sqlc',
]);
run(process.execPath, [
  'node_modules/eslint/bin/eslint.js',
  '.',
  '--max-warnings',
  '0',
]);
run(process.execPath, [
  'node_modules/prettier/bin/prettier.cjs',
  '--check',
  '**/*.{js,mjs,html,json,yml,yaml,md}',
]);
run(process.execPath, ['--test', 'scripts/fqa/client.test.mjs']);
run(process.env.PYTHON || (isWindows ? 'python' : 'python3'), [
  '-m',
  'unittest',
  'discover',
  '-s',
  'scripts/fqa',
  '-p',
  'test_*.py',
]);
run('go', ['mod', 'verify']);
run('go', ['-C', 'tools', 'mod', 'verify']);
run('go', ['-C', 'tools/sqlc', 'mod', 'verify']);
run('go', ['mod', 'verify'], migrateRoot);
run('go', ['vet', ...packages]);
run('go', ['vet', './...'], migrateRoot);
run('go', ['build', ...packages]);
run('go', ['build', './...'], migrateRoot);

if (isWindows) {
  // Windows machines need no C compiler; race tests run in the Linux test image.
  // This network and DSN refer only to the repository's isolated Compose stand.
  run('docker', [
    '--context',
    'desktop-linux',
    'build',
    '--target',
    'test',
    '-t',
    'synthetic-qa-zns-tests:local',
    '.',
  ]);
  run('docker', [
    '--context',
    'desktop-linux',
    'run',
    '--rm',
    '--network',
    'synthetic-qa-zns-sandbox_sandbox',
    '-e',
    'TEST_DATABASE_URL=postgres://postgres:sandbox-owner-only@postgres/zns?sslmode=disable',
    'synthetic-qa-zns-tests:local',
    'go',
    'test',
    ...testArguments,
    ...packages,
  ]);
  run('docker', [
    '--context',
    'desktop-linux',
    'run',
    '--rm',
    '--network',
    'synthetic-qa-zns-sandbox_sandbox',
    '--mount',
    `type=bind,source=${root},target=/workspace/platform,readonly`,
    '--mount',
    `type=bind,source=${migrateRoot},target=/workspace/tools/migrate,readonly`,
    '--workdir',
    '/workspace/tools/migrate',
    '-e',
    'TEST_DATABASE_URL=postgres://postgres:sandbox-owner-only@postgres/zns?sslmode=disable',
    'synthetic-qa-zns-tests:local',
    'go',
    'test',
    ...testArguments,
    './...',
  ]);
} else {
  run('go', ['test', ...testArguments, ...packages]);
  run('go', ['test', ...testArguments, './...'], migrateRoot);
}
run('go', [
  'test',
  './internal/identity',
  '-run',
  '^$',
  '-fuzz',
  'FuzzVerify',
  '-fuzztime',
  '10s',
]);
run('go', ['test', './integration', '-run', '^TestLiveSandbox$', '-count=1']);
run(process.execPath, ['tests/browser.mjs']);
run(process.execPath, ['tests/orders.mjs']);
run(process.execPath, ['tests/meals.mjs']);
run(process.execPath, ['tests/proofs.mjs']);
run(process.execPath, ['tests/notifications.mjs']);
run(process.execPath, ['tests/exports.mjs']);
run(process.execPath, ['tests/fqa.mjs']);
run(process.execPath, ['tests/language-payments.mjs']);
run(process.execPath, ['tests/profiles.mjs']);
run(process.execPath, ['tests/media.mjs']);
run(process.execPath, ['tests/av-player.mjs']);
run(process.execPath, ['tests/av-worker.mjs']);
console.log(
  'All local quality gates passed. Independent Code QA and functional QA are still required.',
);

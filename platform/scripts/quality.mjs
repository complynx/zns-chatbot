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
const isWindows = process.platform === 'win32';
const packages = ['./cmd/...', './internal/...', './integration/...'];
// Each integration case migrates its own database. Bound parallel migrations
// independently of host CPU count; concurrency tests still run their actors.
const testArguments = ['-race', '-count=1', '-p', '2', '-parallel', '4'];

function run(command, arguments_) {
  const result = spawnSync(command, arguments_, {
    cwd: root,
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
run(vulnerabilityBinary, ['./cmd/...', './internal/...']);
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
run('go', ['vet', ...packages]);
run('go', ['build', './cmd/...']);

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
    'zns-sandbox-tests:local',
    '.',
  ]);
  run('docker', [
    '--context',
    'desktop-linux',
    'run',
    '--rm',
    '--network',
    'zns-sandbox_sandbox',
    '-e',
    'TEST_DATABASE_URL=postgres://postgres:sandbox-owner-only@postgres/zns?sslmode=disable',
    'zns-sandbox-tests:local',
    'go',
    'test',
    ...testArguments,
    ...packages,
  ]);
} else {
  run('go', ['test', ...testArguments, ...packages]);
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

import { spawnSync } from 'node:child_process';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import manifest from './slow-tests.json' with { type: 'json' };

export const packages = [
  './cmd/...',
  './internal/...',
  './integration/...',
  './identityprovision/...',
  './deploy/...',
];
export const testArguments = ['-race', '-count=1', '-p', '2', '-parallel', '4'];
const root = fileURLToPath(new URL('..', import.meta.url));

// Unknown paths include shared dependencies and select every slow scenario.
export function affectedGroups(paths, groups = manifest) {
  const selected = new Set();
  for (const changedPath of paths) {
    const matches = groups.filter((group) =>
      group.affected.some((prefix) => changedPath.startsWith(prefix)),
    );
    if (matches.length === 0) return groups;
    for (const group of matches) selected.add(group);
  }
  return [...selected];
}

export function discover(output) {
  const tests = new Map();
  for (const line of output.trim().split('\n')) {
    const event = JSON.parse(line);
    if (!event.Package) continue;
    if (!tests.has(event.Package)) tests.set(event.Package, new Set());
    const name = event.Output?.trim();
    if (/^(?:Test|Example|Fuzz)[\p{L}\p{N}_]*$/u.test(name))
      tests.get(event.Package).add(name);
  }
  if (tests.size === 0)
    throw new Error('Go test discovery returned no packages');
  return tests;
}

export function plan(
  tests,
  moduleName,
  mode,
  groups = manifest,
  selected = groups,
) {
  const slow = new Map();
  const wanted = new Map();
  for (const group of groups) {
    const packageName = `${moduleName}/${group.package}`;
    const names = tests.get(packageName);
    if (!names) throw new Error(`Missing slow-test package: ${packageName}`);
    for (const selector of group.selectors) {
      if (!/^\^(?:Test|Example|Fuzz)[\p{L}\p{N}_]+\$?$/u.test(selector))
        throw new Error(`Invalid slow-test selector: ${selector}`);
      const prefix = selector.slice(1);
      const matches = [...names].filter((name) =>
        prefix.endsWith('$')
          ? name === prefix.slice(0, -1)
          : name.startsWith(prefix),
      );
      if (matches.length === 0)
        throw new Error(
          `Missing slow-test selector: ${packageName} ${selector}`,
        );
      if (!slow.has(packageName)) slow.set(packageName, new Set());
      if (!wanted.has(packageName)) wanted.set(packageName, new Set());
      for (const name of matches) {
        slow.get(packageName).add(name);
        if (selected.includes(group)) wanted.get(packageName).add(name);
      }
    }
  }
  return [...tests].flatMap(([packageName, names]) => {
    const chosen = [...names].filter((name) => {
      const isSlow = slow.get(packageName)?.has(name);
      return (
        mode === 'full' ||
        (mode === 'fast' && !isSlow) ||
        (mode === 'slow' && isSlow) ||
        (mode === 'changed' && (!isSlow || wanted.get(packageName)?.has(name)))
      );
    });
    return chosen.length === 0
      ? []
      : [
          {
            package: packageName,
            names: chosen.toSorted((left, right) =>
              left.localeCompare(right, 'en'),
            ),
            run: `^(${chosen.join('|')})$`,
          },
        ];
  });
}

function capture(command, arguments_) {
  const result = spawnSync(command, arguments_, {
    cwd: root,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.error) throw result.error;
  if (result.status !== 0)
    throw new Error(`${command} failed: ${result.status}\n${result.stderr}`);
  return result.stdout;
}

function changedPaths(base) {
  const baseCommit = capture('git', [
    'rev-parse',
    '--verify',
    '--end-of-options',
    base + '^{commit}',
  ]).trim();
  const committed = capture('git', [
    'diff',
    '--name-only',
    '-z',
    `${baseCommit}...HEAD`,
  ]);
  const working = capture('git', ['diff', '--name-only', '-z', 'HEAD']);
  const untracked = capture('git', [
    'ls-files',
    '--others',
    '--exclude-standard',
    '-z',
  ]);
  return [
    ...new Set(
      `${committed}${working}${untracked}`.split('\0').filter(Boolean),
    ),
  ];
}

export function main(arguments_ = process.argv.slice(2)) {
  if (process.platform !== 'linux')
    throw new Error('Run test tiers in Linux Docker or WSL');
  const [mode, ...options] = arguments_;
  if (!['fast', 'slow', 'changed', 'full'].includes(mode))
    throw new Error(
      'Expected fast, slow, changed BASE, or full; optional --plan',
    );
  const dryRun = options.includes('--plan');
  const parameters = options.filter((option) => option !== '--plan');
  if (parameters.length !== (mode === 'changed' ? 1 : 0))
    throw new Error(
      'changed requires exactly one Git base; other modes take no base',
    );
  if (!dryRun && !process.env.TEST_DATABASE_URL)
    throw new Error(
      'TEST_DATABASE_URL is required; database tests must execute',
    );
  if (!dryRun && !process.env.TEST_CREDIT_UPGRADE_DATABASE_URL)
    throw new Error(
      'TEST_CREDIT_UPGRADE_DATABASE_URL must point to a separate cluster',
    );
  const selected =
    mode === 'changed' ? affectedGroups(changedPaths(parameters[0])) : manifest;
  const moduleName = capture('go', ['list', '-m']).trim();
  const tests = discover(
    capture('go', ['test', '-list', '.', '-json', '-p', '2', ...packages]),
  );
  const commands = plan(tests, moduleName, mode, manifest, selected);
  console.log(JSON.stringify({ mode, commands }, undefined, 2));
  if (dryRun) return;
  for (const command of commands) {
    const result = spawnSync(
      'go',
      ['test', ...testArguments, command.package, '-run', command.run],
      {
        cwd: root,
        stdio: 'inherit',
      },
    );
    if (result.error) throw result.error;
    if (result.status !== 0)
      throw new Error(`Go tests failed: ${command.package} ${result.status}`);
  }
  // Keep the second Go module in every ordinary feedback run.
  const migrate = spawnSync(
    'go',
    ['-C', '../tools/migrate', 'test', ...testArguments, './...'],
    {
      cwd: root,
      stdio: 'inherit',
    },
  );
  if (migrate.error) throw migrate.error;
  if (migrate.status !== 0)
    throw new Error(`Migration tests failed: ${migrate.status}`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main();

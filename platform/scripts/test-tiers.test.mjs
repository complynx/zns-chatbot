import assert from 'node:assert/strict';
import test from 'node:test';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import manifest from './slow-tests.json' with { type: 'json' };
import {
  affectedGroups,
  discover,
  plan,
  testArguments,
} from './test-tiers.mjs';

const groups = [
  {
    package: 'internal/a',
    selectors: ['^TestWait$'],
    affected: ['platform/internal/a/'],
  },
  {
    package: 'internal/b',
    selectors: ['^TestRetry'],
    affected: ['platform/internal/b/'],
  },
];
const discovered = new Map([
  [
    'module/internal/a',
    new Set(['TestWait', 'TestQuick', 'ExampleValue', 'FuzzValue']),
  ],
  ['module/internal/b', new Set(['TestRetryOne', 'TestQuick'])],
]);

test('fast and slow form an exact partition and preserve examples, fuzz seeds and subtests', () => {
  const fast = plan(discovered, 'module', 'fast', groups);
  const slow = plan(discovered, 'module', 'slow', groups);
  const full = plan(discovered, 'module', 'full', groups);
  for (const command of full) {
    const fastNames =
      fast.find((entry) => entry.package === command.package)?.names || [];
    const slowNames =
      slow.find((entry) => entry.package === command.package)?.names || [];
    assert.deepEqual(
      [...fastNames, ...slowNames].toSorted((left, right) =>
        left.localeCompare(right, 'en'),
      ),
      command.names,
    );
    assert.equal(
      new Set([...fastNames, ...slowNames]).size,
      command.names.length,
    );
  }
  assert.deepEqual(fast[0].names, ['ExampleValue', 'FuzzValue', 'TestQuick']);
  assert.equal(slow[0].run, '^(TestWait)$');
  assert.deepEqual(testArguments, [
    '-race',
    '-count=1',
    '-p',
    '2',
    '-parallel',
    '4',
  ]);
});

test('changed retains all fast tests and the affected slow scenarios', () => {
  const selected = affectedGroups(['platform/internal/a/changed.go'], groups);
  assert.deepEqual(selected, [groups[0]]);
  const commands = plan(discovered, 'module', 'changed', groups, selected);
  assert.deepEqual(
    commands[0].names,
    [...discovered.get('module/internal/a')].toSorted((left, right) =>
      left.localeCompare(right, 'en'),
    ),
  );
  assert.deepEqual(commands[1].names, ['TestQuick']);
});

test('shared or unknown changes conservatively select all slow scenarios', () => {
  for (const changedPath of [
    'platform/internal/store/query.sql',
    'platform/internal/delivery/policy.go',
    'platform/internal/config/config.go',
    'platform/schema/094.sql',
    'platform/internal/clock/clock.go',
    'platform/go.mod',
    'unexpected/file',
  ]) {
    assert.deepEqual(affectedGroups([changedPath], groups), groups);
  }
  assert.deepEqual(affectedGroups([], groups), []);
});

test('deleted tests and missing discovery fail closed', () => {
  assert.throws(
    () => plan(new Map(), 'module', 'fast', groups),
    /Missing slow-test package/,
  );
  assert.throws(
    () =>
      plan(
        new Map([['module/internal/a', new Set(['TestQuick'])]]),
        'module',
        'fast',
        groups,
      ),
    /Missing slow-test selector/,
  );
  assert.throws(() => discover(''), SyntaxError);
  assert.throws(() => discover('{}'), /no packages/);
  assert.throws(() => discover('broken'), SyntaxError);
});

test('JSON discovery preserves every default runnable Go test kind', () => {
  const events = [
    'TestOne',
    'FuzzOne',
    'ExampleOne',
    'BenchmarkOne',
    'ok module/internal/a',
  ].map((name) =>
    JSON.stringify({
      Package: 'module/internal/a',
      Action: 'output',
      Output: `${name}\n`,
    }),
  );
  assert.deepEqual(
    [...discover(events.join('\n')).get('module/internal/a')],
    ['TestOne', 'FuzzOne', 'ExampleOne'],
  );
});

const executableFixture = String.raw`#!/usr/bin/env node
const fs = require('node:fs');
const path = require('node:path');
const args = process.argv.slice(2);
const command = path.basename(process.argv[1]);
fs.appendFileSync(process.env.FIXTURE_LOG, JSON.stringify({command,args})+'\n');
if(command==='git') {
  if(args[0]==='rev-parse') console.log('0123456789abcdef');
  else if(args[0]==='diff' && args.includes('0123456789abcdef...HEAD')) process.stdout.write((process.env.FIXTURE_CHANGED || '')+'\0');
  process.exit(0);
}
if(args[0]==='list') { console.log('module'); process.exit(0); }
if(args.includes('-list')) {
  if(process.env.FIXTURE_FAIL==='discovery') process.exit(9);
  process.stdout.write(process.env.FIXTURE_EVENTS); process.exit(0);
}
if(process.env.FIXTURE_FAIL==='tests' && args[0]==='test') process.exit(8);
if(process.env.FIXTURE_FAIL==='migrate' && args[0]==='-C') process.exit(7);
`;

async function cliFixture(t, mode, overrides = {}) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'zns-test-tier-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const previousDirectory = process.cwd();
  process.chdir(directory);
  try {
    await fs.writeFile('go', executableFixture, { mode: 0o755 });
    await fs.writeFile('git', executableFixture, { mode: 0o755 });
  } finally {
    process.chdir(previousDirectory);
  }
  const fixtures = new Map();
  for (const group of manifest) {
    const key = `module/${group.package}`;
    if (!fixtures.has(key))
      fixtures.set(key, new Set(['TestQuick', 'ExampleValue', 'FuzzValue']));
    for (const selector of group.selectors)
      fixtures
        .get(key)
        .add(
          selector.endsWith('$')
            ? selector.slice(1, -1)
            : selector.slice(1) + 'Synthetic',
        );
  }
  const events = [...fixtures].flatMap(([packageName, names]) =>
    [...names].map((name) =>
      JSON.stringify({
        Package: packageName,
        Action: 'output',
        Output: name + '\n',
      }),
    ),
  );
  const log = path.join(directory, 'commands.jsonl');
  const result = spawnSync(
    process.execPath,
    [
      fileURLToPath(new URL('test-tiers.mjs', import.meta.url)),
      mode,
      ...(mode === 'changed' ? ['base'] : []),
    ],
    {
      encoding: 'utf8',
      env: {
        ...process.env,
        PATH: directory + path.delimiter + process.env.PATH,
        TEST_DATABASE_URL: 'postgres://synthetic/unused',
        TEST_CREDIT_UPGRADE_DATABASE_URL: 'postgres://synthetic/unused-credit',
        FIXTURE_LOG: log,
        FIXTURE_EVENTS: events.join('\n'),
        ...overrides,
      },
    },
  );
  assert.equal(result.error, undefined);
  process.chdir(directory);
  try {
    const logText = await fs.readFile('commands.jsonl', 'utf8');
    const calls = logText
      .trim()
      .split('\n')
      .map((line) => JSON.parse(line));
    return { result, calls, fixtures };
  } finally {
    process.chdir(previousDirectory);
  }
}

test('CLI forwards every tier to Go and always executes the migrate module', async (t) => {
  for (const mode of ['fast', 'slow', 'changed', 'full']) {
    const { result, calls, fixtures } = await cliFixture(t, mode, {
      FIXTURE_CHANGED: 'platform/internal/identity/changed.go',
    });
    assert.equal(result.status, 0, result.stderr);
    const receipt = JSON.parse(result.stdout);
    const selected =
      mode === 'changed'
        ? affectedGroups(['platform/internal/identity/changed.go'])
        : manifest;
    assert.deepEqual(
      receipt.commands,
      plan(fixtures, 'module', mode, manifest, selected),
    );
    const executions = calls.filter(
      (call) =>
        call.command === 'go' &&
        !call.args.includes('-list') &&
        call.args[0] !== 'list',
    );
    assert.deepEqual(
      executions.map((call) => call.args),
      [
        ...receipt.commands.map((command) => [
          'test',
          ...testArguments,
          command.package,
          '-run',
          command.run,
        ]),
        ['-C', '../tools/migrate', 'test', ...testArguments, './...'],
      ],
    );
  }
});

test('CLI failures exit nonzero and stop remaining subprocesses', async (t) => {
  for (const failure of ['discovery', 'tests', 'migrate']) {
    const { result, calls } = await cliFixture(t, 'full', {
      FIXTURE_FAIL: failure,
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /failed/);
    if (failure !== 'migrate')
      assert.equal(
        calls.some((call) => call.args[0] === '-C'),
        false,
      );
  }
});

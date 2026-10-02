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
  changedPaths,
  discover,
  packages,
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

test('intentional eligibility, event and receipt waits stay in the slow tier', () => {
  const waits = new Map([
    ['TestPassRedactionRetriesAfterActualDeadline', 'passbooking'],
    ['TestRuntimeAuthorityTerminalNoticeDeliveryRecovery', 'passbooking'],
    ['TestPassTakeoverConcurrentReplayAndNoticeCurrentness', 'passbooking'],
    ['TestPassNoticeDeliveryLocaleHistoryAndRetry', 'passbooking'],
    ['TestPassNoticeStaleRetryKeepsOriginalHistory', 'passbooking'],
    ['TestPaymentUnavailableContextKeepsExactTarget', 'orders'],
    ['TestModernExportRetriesAuthorityOutageBeforeTransport', 'orders'],
    ['TestAdminMessageProviderDeliveryBoundary', 'adminmessage'],
    ['TestBotDeliveryReceiptAuthenticatedRecovery', 'identity'],
    ['TestBotDeliveryReceiptDeniedBatchCannotStarveHealthyOwner', 'identity'],
    ['TestRecoveredCanonicalNegativeReplayPreservesSchedule', 'passbooking'],
    ['TestPassBookingRechecksFinishAfterLock', 'passbooking'],
    ['TestRegistrationFixtureRealStateAndRevocation', 'passbooking'],
    ['TestKnowledgePriorityAndDynamicEventEnd', 'knowledge'],
  ]);
  for (const [name, domain] of waits) {
    const selected = affectedGroups([`platform/internal/${domain}/changed.go`]);
    assert.equal(
      selected.some(
        (group) =>
          group.package === 'integration' &&
          group.selectors.includes(`^${name}$`),
      ),
      true,
      `${name} must run after ${domain} changes`,
    );
  }
});

test('short budget probes remain fast beside expensive execution waits', () => {
  for (const [packageName, slowNames, fastNames] of [
    [
      'internal/sandbox',
      ['TestModelControlHoldExpiresWithoutDetachedWaiter'],
      [
        'TestModelInstallAdmissionEndsBeforeBlockedMutation',
        'TestModelConsumptionSuccessfulSaveCompletionExpiresUnderControlLock',
      ],
    ],
    [
      'internal/bot',
      [
        'TestBotTransportRetryCapturedLanguageCard',
        'TestBotDeliveryPostgresRecoveryRetainsOrderAndAttemptFence',
        'TestAcknowledgeControlOutcomes',
      ],
      [
        'TestAcknowledgeRequestDeadline',
        'TestStartAgentDiagnosticsOwnTimeoutIsOmitted',
      ],
    ],
    [
      'internal/scriptclient',
      ['TestComposedRPCAllowsBoundedHostWorkBeyondFiveSeconds'],
      [
        'TestHostOperationDeadlineStopsRun',
        'TestCallbacksShareEarlierWholeRunDeadline',
        'TestExpiredCallbackDoesNotPublishLateReceiptOrRepeatEffect',
      ],
    ],
    [
      'internal/scriptworker',
      ['TestExecuteBudgetsAndDataBoundary'],
      [
        'TestExecuteHostWaitDoesNotUseComputeBudget',
        'TestExecuteSequentialCallbackWallBudget',
      ],
    ],
  ]) {
    const scoped = manifest.filter((group) => group.package === packageName);
    const fixture = new Map([
      [`module/${packageName}`, new Set([...slowNames, ...fastNames])],
    ]);
    assert.deepEqual(
      plan(fixture, 'module', 'fast', scoped)[0].names,
      fastNames.toSorted((left, right) => left.localeCompare(right, 'en')),
    );
    assert.deepEqual(
      plan(fixture, 'module', 'slow', scoped)[0].names,
      slowNames.toSorted((left, right) => left.localeCompare(right, 'en')),
    );
  }
});

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
    'platform/internal/sandbox/model_fixture_control.go',
    'platform/internal/bot/bot.go',
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
  if(args.includes('--show-toplevel')) console.log(process.cwd());
  else if(args[0]==='rev-parse') console.log('0123456789abcdef');
  else if(args[0]==='diff' && args.includes('0123456789abcdef...HEAD')) process.stdout.write((process.env.FIXTURE_CHANGED || '')+'\0');
  else if(args.includes('--cached')) process.stdout.write((process.env.FIXTURE_STAGED || '')+'\0');
  else if(args[0]==='diff') process.stdout.write((process.env.FIXTURE_WORKING || '')+'\0');
  else if(args[0]==='ls-files') process.stdout.write((process.env.FIXTURE_UNTRACKED || '')+'\0');
  process.exit(0);
}
if(args[0]==='list') {
  if(args.includes('-m')) console.log('module');
  else if(args.includes('./...')) process.stdout.write(process.env.FIXTURE_PACKAGES);
  else process.exit(6);
  process.exit(0);
}
if(args.includes('-list')) {
  if(!args.includes('module/importdelivery') || args.some(arg=>arg.startsWith('module/node_modules/'))) process.exit(6);
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
  fixtures.set(
    'module/importdelivery',
    new Set(['TestMassageRegistrationValidatesBeforeWriting']),
  );
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
        FIXTURE_PACKAGES: [
          ...fixtures.keys(),
          'module/node_modules/flatted/golang/pkg/flatted',
        ].join('\n'),
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
    assert.deepEqual(packages, ['./...']);
    const importTests = receipt.commands.find(
      (command) => command.package === 'module/importdelivery',
    );
    assert.deepEqual(
      importTests?.names,
      mode === 'slow'
        ? undefined
        : ['TestMassageRegistrationValidatesBeforeWriting'],
    );
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

test('cross-domain changes select notification retry and source recovery scenarios', () => {
  for (const [domain, sourceTest] of [
    ['legacyfood', '^TestFoodCSVPartialRetryAndRevokedGrant$'],
    ['passbooking', '^TestPassPlanCommittedStatusRetry$'],
    ['orders', '^TestModernExportRetriesAuthorityOutageBeforeTransport$'],
  ]) {
    const selected = affectedGroups([`platform/internal/${domain}/changed.go`]);
    assert.equal(
      selected.some((group) =>
        group.selectors.includes('^TestNotificationUncertainRetry'),
      ),
      true,
    );
    assert.equal(
      selected.some((group) => group.selectors.includes(sourceTest)),
      true,
    );
  }
  for (const domain of [
    'identity',
    'conversation',
    'scriptclient',
    'scriptprotocol',
    'scriptworker',
    'workflow',
  ])
    assert.deepEqual(
      affectedGroups([`platform/internal/${domain}/changed.go`]),
      manifest,
    );
  for (const selector of [
    '^TestPassTakeoverConcurrentReplayAndNoticeCurrentness$',
    '^TestWorkflowUnavailableNoticeLocaleAndReplay$',
    '^TestExportDoesNotMixEventsAndRetriesTelegramFailure$',
    '^TestRegistrationFixtureRealStateAndRevocation$',
  ])
    assert.equal(
      affectedGroups(['platform/internal/orders/changed.go']).some((group) =>
        group.selectors.includes(selector),
      ),
      true,
      `${selector} must run after orders changes`,
    );
});

test('CLI independently includes staged, unstaged and untracked inventories', async (t) => {
  for (const field of [
    'FIXTURE_STAGED',
    'FIXTURE_WORKING',
    'FIXTURE_UNTRACKED',
  ]) {
    const { result } = await cliFixture(t, 'changed', {
      [field]: 'tools/migrate/new.go',
    });
    assert.equal(result.status, 0, result.stderr);
    const receipt = JSON.parse(result.stdout);
    assert.equal(
      receipt.commands
        .find((command) => command.package === 'module/internal/bot')
        .names.some((name) => name.startsWith('TestBotTransportRetry')),
      true,
    );
  }
});

test('real Git preserves canceled index edits and root-scoped untracked paths', async (t) => {
  const directory = await fs.mkdtemp(
    path.join(os.tmpdir(), 'zns-test-tier-git-'),
  );
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const previousDirectory = process.cwd();
  process.chdir(directory);
  const git = (arguments_) => {
    const result = spawnSync('git', arguments_, {
      cwd: directory,
      encoding: 'utf8',
    });
    assert.equal(result.status, 0, result.stderr);
    return result.stdout;
  };
  try {
    await fs.mkdir('platform/internal/identity', { recursive: true });
    await fs.mkdir('platform/internal/adminmessage', { recursive: true });
    await fs.mkdir('tools/migrate', { recursive: true });
    await fs.writeFile('platform/internal/identity/actor.go', 'original\n');
    git(['init', '--quiet']);
    git(['add', '.']);
    git([
      '-c',
      'user.name=Synthetic fixture',
      '-c',
      'user.email=fixture@example.invalid',
      '-c',
      'commit.gpgsign=false',
      'commit',
      '--quiet',
      '-m',
      'fixture',
    ]);
    const platformDirectory = path.join(directory, 'platform');
    assert.deepEqual(changedPaths('HEAD', platformDirectory), []);
    await fs.writeFile('platform/internal/identity/actor.go', 'staged\n');
    git(['add', '.']);
    await fs.writeFile('platform/internal/identity/actor.go', 'original\n');
    assert.equal(git(['diff', '--name-only', 'HEAD']), '');
    assert.deepEqual(changedPaths('HEAD', platformDirectory), [
      'platform/internal/identity/actor.go',
    ]);
    git(['add', '.']);
    await fs.writeFile('platform/internal/adminmessage/new.go', 'untracked\n');
    const domainPaths = changedPaths('HEAD', platformDirectory);
    assert.deepEqual(domainPaths, ['platform/internal/adminmessage/new.go']);
    assert.equal(
      affectedGroups(domainPaths).some((group) =>
        group.selectors.includes('^TestNotificationUncertainRetry'),
      ),
      true,
    );
    assert.equal(
      affectedGroups(domainPaths).some((group) =>
        group.selectors.includes('^TestAdminMessageProviderDeliveryBoundary$'),
      ),
      true,
    );
    await fs.writeFile('tools/migrate/new.go', 'untracked shared\n');
    const sharedPaths = changedPaths('HEAD', platformDirectory);
    assert.deepEqual(sharedPaths, [
      'platform/internal/adminmessage/new.go',
      'tools/migrate/new.go',
    ]);
    assert.deepEqual(affectedGroups(sharedPaths), manifest);
    await fs.rename(
      'platform/internal/identity/actor.go',
      'platform/internal/adminmessage/moved.go',
    );
    git([
      'add',
      'platform/internal/identity/actor.go',
      'platform/internal/adminmessage/moved.go',
    ]);
    const renamedPaths = changedPaths('HEAD', platformDirectory);
    assert.equal(
      renamedPaths.includes('platform/internal/identity/actor.go'),
      true,
    );
    assert.equal(
      renamedPaths.includes('platform/internal/adminmessage/moved.go'),
      true,
    );
  } finally {
    process.chdir(previousDirectory);
  }
});

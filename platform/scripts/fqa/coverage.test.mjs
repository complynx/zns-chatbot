import assert from 'node:assert/strict';
import {
  mkdtemp,
  mkdir,
  writeFile,
  rm,
  symlink,
  readFile,
} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { validateCoverage, validEvidencePath } from './coverage.mjs';

const candidate = 'sha256:synthetic-candidate';
const cliURL = new URL('coverage-cli.mjs', import.meta.url);

function assertInputFailure(result, message) {
  assert.equal(result.status, 1);
  assert.equal(result.stderr, '');
  const summary = JSON.parse(result.stdout);
  assert.equal(summary.complete, false);
  assert.equal(summary.required, 0);
  assert.equal(summary.passed, 0);
  assert.equal(summary.errors.length, 1);
  assert.match(summary.errors[0], message);
}

const separators = ['/', '\\'];
const prefixes = separators.flatMap((left) =>
  separators.map((right) => left + right),
);
const deviceNames = [
  'NUL',
  'CON.txt',
  'AUX',
  'PRN',
  'COM1',
  'LPT9',
  'CONIN$',
  'CONOUT$',
  'conin$',
  'conout$.txt',
  'NUL .txt',
  'COM1  .log',
];
for (const digit of ['¹', '²', '³'])
  deviceNames.push(`COM${digit}.txt`, `LPT${digit}.txt`);
const unsafeInputs = [
  ...prefixes.map((prefix) => `${prefix}unused.invalid/share/file`),
  'C:/qa/catalog.json:stream',
  'relative/file:stream',
  ...deviceNames.map((name) => `C:/qa/${name}`),
];
for (const unsafeInput of unsafeInputs) {
  for (const option of ['catalog', 'results', 'evidence-root']) {
    test(`CLI rejects ${JSON.stringify(unsafeInput)} in ${option} before filesystem access`, () => {
      const inputs = [
        ['catalog', path.resolve('unused-catalog.json')],
        ['results', path.resolve('unused-results.json')],
        ['evidence-root', path.resolve('unused-evidence')],
        ['candidate', candidate],
      ];
      const arguments_ = inputs.flatMap(([key, value]) => [
        `--${key}`,
        key === option ? unsafeInput : value,
      ]);
      // Replace the filesystem APIs before importing the CLI; no network path is opened.
      const script = `
        import fs from 'node:fs/promises';
        import { syncBuiltinESMExports } from 'node:module';
        import assert from 'node:assert/strict';
        let calls = 0;
        const forbidden = () => { calls++; throw new Error('Unexpected filesystem access'); };
        fs.lstat = forbidden;
        fs.readFile = forbidden;
        syncBuiltinESMExports();
        // Node eval mode parses arguments after argv[0], without a script filename.
        process.argv = [process.execPath, ...${JSON.stringify(arguments_)}];
        await import(${JSON.stringify(cliURL.href)});
        assert.equal(calls, 0);
      `;
      const result = spawnSync(
        process.execPath,
        ['--input-type=module', '--eval', script],
        { encoding: 'utf8' },
      );
      assertInputFailure(
        result,
        /Explicit local filesystem paths are required/,
      );
    });
  }
}

test('CLI argument errors return structured JSON', () => {
  for (const arguments_ of [[], ['--unknown']]) {
    const result = spawnSync(
      process.execPath,
      [fileURLToPath(cliURL), ...arguments_],
      { encoding: 'utf8' },
    );
    assertInputFailure(result, /Usage:|Unknown option/);
  }
});

function fixture() {
  const cells = ['en', 'ru'].flatMap((locale) =>
    ['mouse', 'touch'].map((input) => ({ locale, input })),
  );
  const catalog = {
    version: 1,
    scenarios: [
      {
        id: 'orders.handoff',
        cells,
        requiredEvidence: ['journey', 'state'],
        resources: ['owner-a'],
        barriers: [],
      },
      {
        id: 'orders.replay',
        cells: [{ locale: 'independent', input: 'none' }],
        requiredEvidence: ['state'],
      },
    ],
  };
  const results = {
    version: 1,
    candidate,
    results: catalog.scenarios.flatMap((scenario) =>
      scenario.cells.map((cell) => ({
        id: scenario.id,
        ...cell,
        candidate,
        status: 'passed',
        errors: [],
        evidence: scenario.requiredEvidence.map((kind) => ({
          kind,
          path: `${kind}.json`,
        })),
        durationsMs: { setup: 0, run: 10, evidence: 1, cleanup: 2 },
      })),
    ),
  };
  return {
    catalog,
    results,
    files: new Map([
      ['journey.json', { size: 2 }],
      ['state.json', { size: 2 }],
    ]),
  };
}
function validate({ catalog, results, files }) {
  return validateCoverage(catalog, results, candidate, files);
}
test('complete synthetic EN/RU mouse/touch matrix plus independent invariant', () => {
  assert.deepEqual(validate(fixture()), {
    complete: true,
    required: 5,
    passed: 5,
    errors: [],
  });
});
test('Stage B seed is structurally valid but cannot pass without results', async () => {
  const catalog = JSON.parse(
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Fixed seed URL beside this test; no caller-supplied path.
    await readFile(new URL('scenarios.stage-b.json', import.meta.url), 'utf8'),
  );
  const summary = validateCoverage(
    catalog,
    { version: 1, candidate, results: [] },
    candidate,
  );
  assert.equal(summary.required, 12);
  assert.equal(summary.complete, false);
  assert.equal(summary.errors.length, 12);
  assert.ok(summary.errors.every((error) => error.endsWith('missing result')));
});
test('empty and malformed top-level data fail closed', () => {
  for (const catalog of [
    { version: 1, scenarios: [] },
    JSON.parse('null'),
    [],
  ]) {
    assert.equal(validateCoverage(catalog, {}, candidate).complete, false);
  }
  assert.equal(
    validateCoverage(fixture().catalog, JSON.parse('null'), candidate).complete,
    false,
  );
});
const negatives = new Map([
  ['missing cell', (value) => value.results.results.pop()],
  [
    'duplicate scenario',
    (value) => {
      value.catalog.scenarios.push(value.catalog.scenarios[0]);
    },
  ],
  [
    'duplicate catalog cell',
    (value) => {
      value.catalog.scenarios[0].cells.push(
        value.catalog.scenarios[0].cells[0],
      );
    },
  ],
  [
    'duplicate result',
    (value) => {
      value.results.results.push(value.results.results[0]);
    },
  ],
  [
    'unknown scenario',
    (value) => {
      value.results.results[0].id = 'unknown';
    },
  ],
  [
    'unknown cell',
    (value) => {
      value.results.results[0].input = 'none';
    },
  ],
  [
    'candidate mismatch',
    (value) => {
      value.results.candidate = 'different';
    },
  ],
  [
    'cell candidate mismatch',
    (value) => {
      value.results.results[0].candidate = 'different';
    },
  ],
  [
    'recorded errors',
    (value) => {
      value.results.results[0].errors = ['page error'];
    },
  ],
  [
    'omitted error ledger',
    (value) => {
      delete value.results.results[0].errors;
    },
  ],
  ['absent evidence', (value) => value.files.clear()],
  ['empty evidence', (value) => value.files.set('journey.json', { size: 0 })],
  [
    'negative evidence size',
    (value) => value.files.set('journey.json', { size: -1 }),
  ],
  [
    'unsafe evidence',
    (value) => value.files.set('journey.json', { error: 'symlink' }),
  ],
  ['missing evidence kind', (value) => value.results.results[0].evidence.pop()],
  [
    'invalid duration',
    (value) => {
      value.results.results[0].durationsMs.run = -1;
    },
  ],
  [
    'invalid resource',
    (value) => {
      value.catalog.scenarios[0].resources = 'owner';
    },
  ],
  [
    'malformed catalog',
    (value) => {
      value.catalog.scenarios[0].cells = [undefined];
    },
  ],
  [
    'malformed result',
    (value) => {
      value.results.results.push(JSON.parse('null'));
    },
  ],
]);
for (const status of ['pending', 'failed', 'blocked', 'skipped', 'unknown'])
  negatives.set(`status ${status}`, (value) => {
    value.results.results[0].status = status;
  });
for (const [name, mutate] of negatives)
  test(`rejects ${name}`, () => {
    const value = fixture();
    mutate(value);
    const result = validate(value);
    assert.equal(result.complete, false);
    assert.ok(result.errors.length > 0);
    if (name !== 'duplicate result') return;
    assert.equal(result.required, 5);
    assert.equal(result.passed, 5);
  });

test('portable paths reject traversal, network, devices and alternate streams', () => {
  for (const value of [
    ...deviceNames,
    '../secret',
    '/secret',
    '//host/share',
    'https://host/file',
    'C:/secret',
    String.raw`a\b`,
    'a//b',
    './a',
    'a/../b',
    'a:stream',
    'NUL.txt',
    'a/COM1',
    'a.',
    'a ',
    'a\u{0}b',
  ])
    assert.equal(validEvidencePath(value), false, value);
  assert.equal(validEvidencePath('en/mouse/trace.zip'), true);
});

test('CLI does not inspect device evidence and does not read nonregular inputs', () => {
  for (const nonregular of ['', 'catalog', 'results']) {
    const value = fixture();
    value.results.results[0].evidence = deviceNames.map((name) => ({
      kind: 'journey',
      path: name,
    }));
    const inputs = new Map([
      [path.resolve('catalog.json'), JSON.stringify(value.catalog)],
      [path.resolve('results.json'), JSON.stringify(value.results)],
    ]);
    const arguments_ = [
      '--catalog',
      path.resolve('catalog.json'),
      '--results',
      path.resolve('results.json'),
      '--evidence-root',
      path.resolve('evidence'),
      '--candidate',
      candidate,
    ];
    // All metadata and reads are mocked; neither devices nor special files are opened.
    const script = `
      import fs from 'node:fs/promises';
      import path from 'node:path';
      import { syncBuiltinESMExports } from 'node:module';
      import assert from 'node:assert/strict';
      const inputs = new Map(${JSON.stringify([...inputs])});
      const devices = new Set(${JSON.stringify(deviceNames)});
      const nonregular = ${JSON.stringify(nonregular)};
      let reads = 0;
      fs.lstat = async (name) => {
        assert.equal(devices.has(path.basename(name)), false, 'Device inspected');
        return {
          isSymbolicLink: () => false,
          isDirectory: () => !inputs.has(name),
          isFile: () => path.basename(name) !== nonregular + '.json',
          size: 2,
        };
      };
      fs.readFile = async (name) => { reads++; return inputs.get(name); };
      syncBuiltinESMExports();
      process.argv = [process.execPath, ...${JSON.stringify(arguments_)}];
      await import(${JSON.stringify(cliURL.href)});
      assert.equal(reads, nonregular ? 0 : 2);
    `;
    const result = spawnSync(
      process.execPath,
      ['--input-type=module', '--eval', script],
      { encoding: 'utf8' },
    );
    if (nonregular) assertInputFailure(result, /regular file/);
    else {
      assert.equal(result.status, 1);
      assert.equal(result.stderr, '');
      assert.match(result.stdout, /invalid evidence record\/path/);
    }
  }
});

test('CLI verifies regular nonempty local files and rejects absent/empty/traversing evidence', async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'fqa-coverage-'));
  try {
    const root = path.join(directory, 'evidence');
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await mkdir(root);
    const value = fixture();
    const catalogFile = path.join(directory, 'catalog.json');
    const resultsFile = path.join(directory, 'results.json');
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(catalogFile, JSON.stringify(value.catalog));
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(path.join(root, 'journey.json'), '{}');
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(path.join(root, 'state.json'), '{}');
    const run = (evidenceRoot = root) =>
      spawnSync(
        process.execPath,
        [
          fileURLToPath(cliURL),
          '--catalog',
          catalogFile,
          '--results',
          resultsFile,
          '--evidence-root',
          evidenceRoot,
          '--candidate',
          candidate,
        ],
        { encoding: 'utf8' },
      );
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(resultsFile, JSON.stringify(value.results));
    assert.equal(run().status, 0);
    assertInputFailure(run(path.join(directory, 'missing')), /ENOENT/);
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; malformed JSON exercises the input failure contract.
    await writeFile(resultsFile, '{');
    assertInputFailure(run(), /JSON|property name/);
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Restore the test-owned report before the independent evidence checks.
    await writeFile(resultsFile, JSON.stringify(value.results));
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(path.join(root, 'state.json'), '');
    assert.equal(run().status, 1);
    await rm(path.join(root, 'state.json'));
    assert.equal(run().status, 1);
    value.results.results[0].evidence[0].path = '../catalog.json';
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(resultsFile, JSON.stringify(value.results));
    assert.equal(run().status, 1);
    // Directory junctions are available without Windows symlink elevation.
    const outside = path.join(directory, 'outside');
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await mkdir(outside);
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(path.join(outside, 'data.json'), '{}');
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await symlink(
      outside,
      path.join(root, 'linked'),
      process.platform === 'win32' ? 'junction' : 'dir',
    );
    value.results.results[0].evidence[0].path = 'linked/data.json';
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(path.join(root, 'state.json'), '{}');
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Test-owned temporary directory; no user-selected write targets.
    await writeFile(resultsFile, JSON.stringify(value.results));
    const linked = run();
    assert.equal(linked.status, 1);
    assert.match(linked.stdout, /unsafe evidence linked\/data.json/);
    const linkedRoot = run(path.join(root, 'linked'));
    assertInputFailure(linkedRoot, /cannot be symbolic links/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

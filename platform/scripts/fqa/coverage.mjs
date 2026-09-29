const statuses = new Set(['pending', 'passed', 'failed', 'blocked', 'skipped']);
const isIdentifier = (value) =>
  typeof value === 'string' &&
  value.split(/[.-]/).every((part) => /^[a-z0-9]+$/.test(part));
const isObject = (value) =>
  value !== null && typeof value === 'object' && !Array.isArray(value);
const strings = (value) =>
  Array.isArray(value) &&
  value.length > 0 &&
  value.every((item) => typeof item === 'string' && item.trim().length > 0) &&
  new Set(value).size === value.length;
const cellKey = (cell) => JSON.stringify([cell.locale, cell.input]);
const isValidCell = (cell) =>
  isObject(cell) &&
  ['en', 'ru', 'independent'].includes(cell.locale) &&
  ['mouse', 'touch', 'none'].includes(cell.input);

// Reject Windows aliases and stream syntax even when validation runs on Unix.
export function isValidPathComponent(value) {
  return (
    value.length > 0 &&
    !/[\\/:<>"|?*]/.test(value) &&
    [...value].every((character) => character.codePointAt(0) >= 32) &&
    !/[. ]$/.test(value) &&
    !/^(?:con|conin\$|conout\$|prn|aux|nul|com[1-9¹²³]|lpt[1-9¹²³]) *(?:\.|$)/i.test(
      value,
    )
  );
}

// Portable relative evidence names only; the CLI also checks filesystem boundaries.
export function validEvidencePath(value) {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    !/[\\:]/.test(value) &&
    [...value].every((character) => character.codePointAt(0) >= 32) &&
    !value.startsWith('/') &&
    value
      .split('/')
      .every(
        (part) =>
          part !== '' &&
          part !== '.' &&
          part !== '..' &&
          isValidPathComponent(part),
      )
  );
}

// Pure completeness validation. Evidence metadata comes from the local CLI, not the report.
export function validateCoverage(
  catalog,
  results,
  candidate,
  evidenceFiles = new Map(),
) {
  const errors = [];
  const scenarios = new Map();
  const seen = new Set();
  let required = 0;
  let passed = 0;
  if (typeof candidate !== 'string' || !candidate.trim())
    errors.push('Expected candidate identity is required');
  if (
    !isObject(catalog) ||
    catalog.version !== 1 ||
    !Array.isArray(catalog.scenarios) ||
    catalog.scenarios.length === 0
  ) {
    return {
      complete: false,
      required,
      passed,
      errors: [...errors, 'Invalid catalog'],
    };
  }
  for (const scenario of catalog.scenarios) {
    if (
      !isObject(scenario) ||
      !isIdentifier(scenario.id) ||
      typeof scenario.id !== 'string' ||
      scenarios.has(scenario.id)
    ) {
      errors.push('Invalid or duplicate scenario ID');
      continue;
    }
    scenarios.set(scenario.id, scenario);
    if (
      !Array.isArray(scenario.cells) ||
      scenario.cells.length === 0 ||
      scenario.cells.some((cell) => !isValidCell(cell)) ||
      new Set(scenario.cells.map((cell) => cellKey(cell))).size !==
        scenario.cells.length ||
      !strings(scenario.requiredEvidence)
    ) {
      errors.push(
        `${scenario.id}: invalid or duplicate cells/evidence requirements`,
      );
      continue;
    }
    required += scenario.cells.length;
    for (const field of ['resources', 'barriers']) {
      const value = Reflect.get(scenario, field);
      if (
        value !== undefined &&
        (!Array.isArray(value) || (value.length > 0 && !strings(value)))
      )
        errors.push(`${scenario.id}: invalid ${field}`);
    }
  }
  if (
    !isObject(results) ||
    results.version !== 1 ||
    results.candidate !== candidate ||
    !Array.isArray(results.results)
  ) {
    return {
      complete: false,
      required,
      passed,
      errors: [...errors, 'Invalid results or candidate mismatch'],
    };
  }
  for (const result of results.results) {
    if (!isObject(result) || !isValidCell(result)) {
      errors.push('Invalid result cell');
      continue;
    }
    const scenario = scenarios.get(result.id);
    const key = JSON.stringify([result.id, result.locale, result.input]);
    const before = errors.length;
    if (seen.has(key)) errors.push(`${key}: duplicate result`);
    seen.add(key);
    if (
      !scenario ||
      !Array.isArray(scenario.cells) ||
      scenario.cells.every(
        (cell) => !(isValidCell(cell) && cellKey(cell) === cellKey(result)),
      )
    ) {
      errors.push(`${key}: unknown scenario/cell`);
      continue;
    }
    if (result.candidate !== candidate)
      errors.push(`${key}: candidate mismatch`);
    if (!statuses.has(result.status)) errors.push(`${key}: invalid status`);
    if (
      !Array.isArray(result.errors) ||
      result.errors.some((error) => typeof error !== 'string' || !error.trim())
    )
      errors.push(`${key}: errors must be an array of nonempty strings`);
    if (result.status === 'passed' && result.errors?.length)
      errors.push(`${key}: pass has recorded errors`);
    if (
      result.durationsMs !== undefined &&
      (!isObject(result.durationsMs) ||
        Object.entries(result.durationsMs).some(
          ([phase, duration]) =>
            !['setup', 'run', 'evidence', 'cleanup'].includes(phase) ||
            !Number.isFinite(duration) ||
            duration < 0,
        ))
    )
      errors.push(`${key}: invalid durationsMs`);
    const kinds = new Set();
    if (!Array.isArray(result.evidence))
      errors.push(`${key}: evidence must be an array`);
    const records = Array.isArray(result.evidence) ? result.evidence : [];
    for (const record of records) {
      if (
        !isObject(record) ||
        typeof record.kind !== 'string' ||
        !record.kind.trim() ||
        !validEvidencePath(record.path)
      ) {
        errors.push(`${key}: invalid evidence record/path`);
        continue;
      }
      const file = evidenceFiles.get(record.path);
      const evidenceSize = file?.size;
      if (
        !file ||
        file.error ||
        !Number.isSafeInteger(evidenceSize) ||
        evidenceSize <= 0
      )
        errors.push(`${key}: missing, empty or unsafe evidence ${record.path}`);
      else kinds.add(record.kind);
    }
    if (result.status !== 'passed')
      errors.push(`${key}: ${result.status} is incomplete`);
    const requiredKinds = Array.isArray(scenario.requiredEvidence)
      ? scenario.requiredEvidence
      : [];
    for (const kind of requiredKinds) {
      if (!kinds.has(kind))
        errors.push(`${key}: missing evidence kind ${kind}`);
    }
    if (errors.length === before && result.status === 'passed') passed++;
  }
  for (const scenario of scenarios.values()) {
    const cells = Array.isArray(scenario.cells) ? scenario.cells : [];
    for (const cell of cells) {
      if (
        isValidCell(cell) &&
        !seen.has(JSON.stringify([scenario.id, cell.locale, cell.input]))
      )
        errors.push(`${scenario.id} ${cellKey(cell)}: missing result`);
    }
  }
  return {
    complete: errors.length === 0 && passed === required,
    required,
    passed,
    errors,
  };
}

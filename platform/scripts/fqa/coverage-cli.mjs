import { lstat, readFile } from 'node:fs/promises';
import path from 'node:path';
import { parseArgs } from 'node:util';
import {
  validEvidencePath,
  isValidPathComponent,
  validateCoverage,
} from './coverage.mjs';

function localPath(value) {
  if (
    !value ||
    /^[\\/]{2}/.test(value) ||
    value
      .replace(/^[a-z]:[\\/]/i, '')
      .split(/[\\/]/)
      .some(
        (part) =>
          part !== '' &&
          part !== '.' &&
          part !== '..' &&
          !isValidPathComponent(part),
      )
  )
    throw new Error('Explicit local filesystem paths are required');
  return path.resolve(value);
}

// Inspect ancestors before opening files so a junction cannot redirect to a share.
async function checkedLocalPath(value, kind) {
  const resolved = localPath(value);
  const parsed = path.parse(resolved);
  let current = parsed.root;
  const parts = resolved.slice(parsed.root.length).split(path.sep);
  for (const [index, part] of parts.entries()) {
    current = path.join(current, part);
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Explicit local input; inspect each ancestor before any file read.
    const info = await lstat(current);
    if (info.isSymbolicLink())
      throw new Error('Local input ancestors cannot be symbolic links');
    const isFile = index === parts.length - 1 && kind === 'file';
    if (isFile ? !info.isFile() : !info.isDirectory())
      throw new Error(
        isFile ? 'Input must be a regular file' : 'Input must be a directory',
      );
  }
  return resolved;
}

async function inspectEvidence(root, relative) {
  try {
    let current = root;
    const parts = relative.split('/');
    for (const [index, part] of parts.entries()) {
      current = path.join(current, part);
      // eslint-disable-next-line security/detect-non-literal-fs-filename -- Portable relative components under the checked root; reject links before descending.
      const info = await lstat(current);
      if (info.isSymbolicLink())
        throw new Error('Symbolic links are not evidence');
      if (index < parts.length - 1 && !info.isDirectory())
        throw new Error('Not a directory');
      if (index === parts.length - 1) {
        if (!info.isFile()) throw new Error('Not a regular file');
        return { size: info.size };
      }
    }
  } catch (error) {
    return { error: error.message };
  }
}

try {
  const { values } = parseArgs({
    options: {
      catalog: { type: 'string' },
      results: { type: 'string' },
      'evidence-root': { type: 'string' },
      candidate: { type: 'string' },
    },
    allowPositionals: false,
  });
  if (!values.candidate)
    throw new Error(
      'Usage: coverage-cli.mjs --catalog FILE --results FILE --evidence-root DIRECTORY --candidate ID',
    );
  // Reject unsafe inputs together, before inspecting any local path.
  const rootPath = localPath(values['evidence-root']);
  const catalogInput = localPath(values.catalog);
  const resultsInput = localPath(values.results);
  const root = await checkedLocalPath(rootPath, 'directory');
  const catalogPath = await checkedLocalPath(catalogInput, 'file');
  const resultsPath = await checkedLocalPath(resultsInput, 'file');
  // eslint-disable-next-line security/detect-non-literal-fs-filename -- Explicit local catalog; all ancestors checked for links before reading.
  const catalog = JSON.parse(await readFile(catalogPath, 'utf8'));
  // eslint-disable-next-line security/detect-non-literal-fs-filename -- Explicit local results; all ancestors checked for links before reading.
  const results = JSON.parse(await readFile(resultsPath, 'utf8'));
  const files = new Map();
  const records = Array.isArray(results?.results) ? results.results : [];
  for (const result of records) {
    const evidence = Array.isArray(result?.evidence) ? result.evidence : [];
    for (const record of evidence) {
      if (validEvidencePath(record?.path) && !files.has(record.path))
        files.set(record.path, await inspectEvidence(root, record.path));
    }
  }
  const summary = validateCoverage(catalog, results, values.candidate, files);
  console.log(JSON.stringify(summary, undefined, 2));
  process.exitCode = summary.complete ? 0 : 1;
} catch (error) {
  console.log(
    JSON.stringify(
      { complete: false, required: 0, passed: 0, errors: [error.message] },
      undefined,
      2,
    ),
  );
  process.exitCode = 1;
}

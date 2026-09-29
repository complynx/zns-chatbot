import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../test-results/fqa/', import.meta.url));
function plainName(name) {
  if (name === '.' || name === '..' || !/^[\w.-]+$/.test(name))
    throw new Error('Evidence name must be a plain filename');
  return name;
}

// This is the only filesystem boundary. Run and artifact names cannot contain paths.
export class Evidence {
  constructor(name) {
    this.directory = path.join(root, plainName(name));
  }
  file(name) {
    return path.join(this.directory, plainName(name));
  }

  async init() {
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Validated single run name below the fixed test-results/fqa root.
    await fs.mkdir(this.directory, { recursive: true });
  }

  async access(name, flags, action) {
    const filename = this.file(name);
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Both path segments validated by Evidence; no caller-supplied path is accepted.
    const handle = await fs.open(filename, flags);
    try {
      return await action(handle);
    } finally {
      await handle.close();
    }
  }

  read(name) {
    return this.access(name, 'r', (handle) => handle.readFile('utf8'));
  }
  write(name, value, flags = 'w') {
    return this.access(name, flags, (handle) => handle.writeFile(value));
  }

  async replace(source, destination) {
    const from = this.file(source),
      to = this.file(destination);
    // eslint-disable-next-line security/detect-non-literal-fs-filename -- Atomic replacement within the same validated run directory.
    await fs.rename(from, to);
  }
}

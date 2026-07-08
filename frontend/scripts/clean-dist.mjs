import { readdir, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const distDir = fileURLToPath(new URL('../dist/', import.meta.url));

try {
  const entries = await readdir(distDir, { withFileTypes: true });
  await Promise.all(entries
    .filter((entry) => entry.name !== '.gitkeep')
    .map((entry) => rm(join(distDir, entry.name), { recursive: true, force: true })));
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}

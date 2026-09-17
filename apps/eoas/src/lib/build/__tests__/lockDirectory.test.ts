import { spawnSync } from 'child_process';
import fs from 'fs-extra';
import os from 'os';
import path from 'path';
import { afterEach, expect, it } from 'vitest';

import { lockDirectory } from '../workspace';

const directories: string[] = [];
afterEach(async () => {
  await Promise.all(directories.splice(0).map(directory => fs.remove(directory)));
});

async function target(): Promise<string> {
  const parent = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-lock-'));
  directories.push(parent);
  return path.join(parent, 'build');
}

// The pid of a process that has already exited.
function deadPid(): number {
  return spawnSync(process.execPath, ['-e', '']).pid;
}

it('lets one build hold a directory until it releases it', async () => {
  const directory = await target();
  const release = await lockDirectory(directory);
  await expect(lockDirectory(directory)).rejects.toThrow('already running');
  await release();
  await (
    await lockDirectory(directory)
  )();
  expect(await fs.readdir(path.dirname(directory))).toEqual([]);
});

it.each([
  ['a free directory', false],
  ['the lock of a dead build', true],
])('gives %s to exactly one of many simultaneous builds', async (_name, stale) => {
  const directory = await target();
  if (stale) {
    await fs.writeFile(`${directory}.lock`, String(deadPid()));
  }
  const attempts = await Promise.allSettled(
    Array.from({ length: 20 }, () => lockDirectory(directory))
  );
  const winners = attempts.filter(attempt => attempt.status === 'fulfilled');
  expect(winners).toHaveLength(1);
  expect(await fs.readFile(`${directory}.lock`, 'utf8')).toBe(String(process.pid));
  expect(await fs.readdir(path.dirname(directory))).toEqual(['build.lock']);
});

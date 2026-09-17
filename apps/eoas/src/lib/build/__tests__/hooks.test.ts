import fs from 'fs-extra';
import os from 'os';
import path from 'path';
import { afterEach, expect, it, vi } from 'vitest';

import { runPostInstallHook } from '../hooks';
import { BuildLog, PhaseLogger } from '../log';
import { BuildInputs } from '../prepare';

const directories: string[] = [];
afterEach(async () => {
  await Promise.all(directories.splice(0).map(directory => fs.remove(directory)));
});

async function projectWith(scripts: Record<string, string>): Promise<string> {
  const working = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-hook-'));
  directories.push(working);
  await fs.writeJson(path.join(working, 'package.json'), { name: 'app', scripts });
  await fs.writeFile(path.join(working, 'package-lock.json'), '{}');
  return working;
}

function buildLog(phase: PhaseLogger): BuildLog {
  return {
    runBuildPhase: (_phase: unknown, work: (log: PhaseLogger) => unknown) => work(phase),
  } as unknown as BuildLog;
}

const phase = (): PhaseLogger => ({
  write: vi.fn(),
  info: vi.fn(),
  warn: vi.fn(),
  markSkipped: vi.fn(),
});
const build = { env: { ...process.env, FROM_SERVER: 'staging' } } as unknown as BuildInputs;

it('runs the post-install script in the build copy with the build environment', async () => {
  const working = await projectWith({
    'eoas-build-post-install': `node -e "require('fs').writeFileSync('ran.txt', process.env.FROM_SERVER)"`,
  });
  await runPostInstallHook(build, working, buildLog(phase()), []);
  expect(await fs.readFile(path.join(working, 'ran.txt'), 'utf8')).toBe('staging');
});

it('skips a project without the script, and points at a script written for another builder', async () => {
  const log = phase();
  const working = await projectWith({ 'eas-build-post-install': 'node -e "process.exit(1)"' });
  await runPostInstallHook(build, working, buildLog(log), []);
  expect(log.markSkipped).toHaveBeenCalled();
  expect(log.info).toHaveBeenCalledWith(expect.stringContaining('eoas-build-post-install'));
});

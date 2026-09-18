import { expect, it, vi } from 'vitest';

import { LogWriter } from '../log';
import { runBuildCommand } from '../run';

function command(script: string): Parameters<typeof runBuildCommand>[0] {
  return {
    title: 'Installing pods',
    command: process.execPath,
    args: ['-e', script],
    cwd: process.cwd(),
    env: process.env,
    silence: { warnAfterMs: 400, stopAfterMs: 1200 },
  };
}

function logger(): LogWriter {
  return { write: vi.fn(), info: vi.fn(), warn: vi.fn() };
}

it('stops a command that prints nothing, with the child processes it started', async () => {
  const log = logger();
  const hung = `require('child_process').spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], { stdio: 'inherit' }); setInterval(() => {}, 1000);`;
  await expect(runBuildCommand(command(hung), log, [])).rejects.toThrow(
    'Installing pods was stopped'
  );
  expect(log.warn).toHaveBeenCalledWith(expect.stringContaining('has printed nothing'));
});

it('leaves a slow command alone while it keeps printing', async () => {
  const log = logger();
  const slow = `let n = 0; const timer = setInterval(() => { console.log(n); if (++n > 12) clearInterval(timer); }, 60);`;
  await runBuildCommand(command(slow), log, []);
  expect(log.warn).not.toHaveBeenCalled();
});

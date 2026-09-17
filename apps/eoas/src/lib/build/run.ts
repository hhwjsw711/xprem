import spawnAsync from '@expo/spawn-async';
import { ChildProcess } from 'child_process';

import { formatBuildError } from './errors';
import { PhaseLogger } from './log';
import { streamBuildOutput } from './output';

export interface BuildCommand {
  title: string;
  command: string;
  args: string[];
  cwd: string;
  env: NodeJS.ProcessEnv;
  // Rewrites or drops (undefined) an output line before it reaches the build log.
  transform?: (line: string) => string | undefined;
  // Warns, then stops the command, once it has printed nothing for this long.
  silence?: { warnAfterMs: number; stopAfterMs: number };
}

let active: ChildProcess | undefined;

// Stops the command in progress, if any, and resolves once it has exited.
export async function terminateBuildCommand(): Promise<void> {
  const child = active;
  if (!child || child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  await new Promise<void>(resolve => {
    const forceKill = setTimeout(() => child.kill('SIGKILL'), 5000);
    child.once('exit', () => {
      clearTimeout(forceKill);
      resolve();
    });
    child.kill('SIGTERM');
  });
}

export async function runBuildCommand(
  { title, command, args, cwd, env, transform, silence }: BuildCommand,
  log: PhaseLogger,
  secrets: string[]
): Promise<void> {
  let stopped = false;
  let timers: NodeJS.Timeout[] = [];
  try {
    const running = spawnAsync(command, args, { cwd, env });
    active = running.child;
    if (silence) {
      const minutes = (ms: number): string => `${Math.round(ms / 60000)} minutes`;
      timers = [
        setTimeout(() => {
          log.warn(`${title} has printed nothing for ${minutes(silence.warnAfterMs)}.`);
        }, silence.warnAfterMs),
        setTimeout(() => {
          stopped = true;
          void stopProcessTree(running.child.pid);
        }, silence.stopAfterMs),
      ];
    }
    const streams = (['stdout', 'stderr'] as const).flatMap(source => {
      const stream = running.child?.[source];
      return stream
        ? [
            streamBuildOutput(stream, secrets, line => {
              timers.forEach(timer => timer.refresh());
              const shown = transform ? transform(line) : line;
              if (shown !== undefined) {
                log.write(shown, source);
              }
            }),
          ]
        : [];
    });
    try {
      await running;
    } finally {
      active = undefined;
      timers.forEach(timer => {
        clearTimeout(timer);
      });
      streams.forEach(stream => {
        stream.close();
      });
    }
  } catch (error) {
    if (stopped && silence) {
      throw new Error(
        `${title} was stopped: it printed nothing for ${Math.round(
          silence.stopAfterMs / 60000
        )} minutes.`
      );
    }
    throw new Error(formatBuildError(title, error, secrets));
  }
}

// A child left alive keeps the output of the command open, so the whole tree is stopped.
async function stopProcessTree(pid?: number): Promise<void> {
  if (!pid) {
    return;
  }
  const { stdout } = await spawnAsync('pgrep', ['-P', String(pid)]).catch(() => ({ stdout: '' }));
  await Promise.all(stdout.split('\n').map(Number).filter(Boolean).map(stopProcessTree));
  try {
    process.kill(pid);
  } catch {
    // Already exited.
  }
}

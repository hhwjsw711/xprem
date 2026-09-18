import fs from 'fs-extra';
import os from 'os';
import path from 'path';
import { afterEach, expect, it, vi } from 'vitest';

import { LogLine, createBuildLog } from '../log';
import { BuildStep, BuildStepResult, LogMarker } from '../steps';
import Log from '../../log';

vi.mock('../../log', () => ({
  default: { log: vi.fn(), warn: vi.fn(), fail: vi.fn(), succeed: vi.fn() },
}));
afterEach(() => vi.useRealTimers());

it('records step IDs, markers, results and durations, including buffered preparation logs', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-steps-'));
  const log = await createBuildLog(directory, 'test');
  try {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-09T10:00:00Z'));
    await log.runStep(BuildStep.PREPARE_PROJECT, async step => {
      step.info('Preparing project');
      vi.advanceTimersByTime(1234);
    });
    const events: LogLine[] = [];
    log.streamTo({
      write: event => {
        events.push(event);
      },
      close: async () => {},
    });
    await log.runStep(BuildStep.PREBUILD, async step => {
      step.markSkipped();
    });
    await log.runStep(BuildStep.POST_INSTALL_HOOK, async step => {
      step.warn('warning');
    });
    expect(events[0]).toMatchObject({
      buildStepDisplayName: 'Prepare project',
      marker: 'START_STEP',
    });
    expect(events[1].buildStepId).toBe(events[0].buildStepId);
    expect(events[2]).toMatchObject({ marker: 'END_STEP', result: 'success', durationMs: 1234 });
    expect(
      events.filter(event => event.marker === LogMarker.END_STEP).map(event => event.result)
    ).toEqual(['success', 'skipped', 'warning']);
    expect(Log.log).toHaveBeenCalledWith('[Prepare project] Preparing project');
    await log.close();
    expect(await fs.readFile(log.path, 'utf8')).toContain('[Prepare project] Preparing project');
  } finally {
    await log.close();
    await fs.remove(directory);
  }
});

it('keeps errors in their step, masks secrets, and closes an interrupted step once', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-steps-'));
  const log = await createBuildLog(directory, 'test');
  const events: LogLine[] = [];
  log.maskSecrets(['password']);
  log.streamTo({
    write: event => {
      events.push(event);
    },
    close: async () => {},
  });
  try {
    await expect(
      log.runStep(BuildStep.BUILD_APK, async step => {
        step.write('password', 'stderr');
        throw new Error('tool failed: password');
      })
    ).rejects.toThrow('tool failed');
    expect(events.find(event => event.source === 'stderr')?.msg).toBe('[REDACTED]');
    expect(events.at(-1)).toMatchObject({ marker: 'END_STEP', result: BuildStepResult.FAIL });
    expect(JSON.stringify(events)).not.toContain('password');
    await log.runStep(BuildStep.BUILD_APK, async () => {
      log.abort();
    });
    const ends = events.filter(event => event.marker === LogMarker.END_STEP);
    expect(ends).toHaveLength(2);
    expect(ends[1].result).toBe('failed');
    expect(ends[1].buildStepId).not.toBe(ends[0].buildStepId);
  } finally {
    await log.close();
    await fs.remove(directory);
  }
});

it('keeps general messages in one named step across buffered and streamed output', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-steps-'));
  const log = await createBuildLog(directory, 'test');
  const events: LogLine[] = [];
  try {
    log.general.info('Build started');
    log.maskSecrets(['secret']);
    log.streamTo({ write: event => events.push(event), close: async () => {} });
    await log.runStep(BuildStep.BUILD_APK, async step => {
      step.write('Compiling');
    });
    log.general.warn('Upload failed: secret');
    const general = events.filter(event => event.buildStepDisplayName === BuildStep.GENERAL);
    expect(general.map(event => event.msg)).toEqual(['Build started', 'Upload failed: [REDACTED]']);
    expect(general[0].buildStepId).toBeTruthy();
    expect(general[1].buildStepId).toBe(general[0].buildStepId);
    expect(events.every(event => event.buildStepId && event.buildStepDisplayName)).toBe(true);
    expect(events.find(event => event.msg === 'Compiling')!.buildStepId).not.toBe(
      general[0].buildStepId
    );
  } finally {
    await log.close();
    await fs.remove(directory);
  }
});

it.each([false, true])(
  'rejects overlapping steps until the active work settles (abort: %s)',
  async abort => {
    const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-steps-'));
    const log = await createBuildLog(directory, 'test');
    const events: LogLine[] = [];
    log.streamTo({ write: event => events.push(event), close: async () => {} });
    let complete!: () => void;
    const pending = new Promise<void>(resolve => {
      complete = resolve;
    });
    const active = log.runStep(BuildStep.BUILD_APK, () => pending);
    const overlappingWork = vi.fn();
    try {
      await expect(log.runStep(BuildStep.BUILD_AAB, overlappingWork)).rejects.toThrow(
        'another step'
      );
      if (abort) {
        log.abort();
        log.abort();
        await expect(log.runStep(BuildStep.BUILD_AAB, overlappingWork)).rejects.toThrow(
          'another step'
        );
      }
      expect(overlappingWork).not.toHaveBeenCalled();
      complete();
      await active;
      await log.runStep(BuildStep.BUILD_IPA, async () => {});
      const starts = events.filter(event => event.marker === LogMarker.START_STEP);
      const ends = events.filter(event => event.marker === LogMarker.END_STEP);
      expect(starts.map(event => event.buildStepDisplayName)).toEqual([
        BuildStep.BUILD_APK,
        BuildStep.BUILD_IPA,
      ]);
      expect(ends.map(event => event.result)).toEqual([
        abort ? BuildStepResult.FAIL : BuildStepResult.SUCCESS,
        BuildStepResult.SUCCESS,
      ]);
      expect(ends.map(event => event.buildStepId)).toEqual(starts.map(event => event.buildStepId));
    } finally {
      complete();
      await active;
      await log.close();
      await fs.remove(directory);
    }
  }
);

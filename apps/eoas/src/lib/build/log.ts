import { randomUUID } from 'crypto';

import { createBuildOutputRedactor } from './errors';
import { openLogFile } from './logFile';
import { BuildStep, BuildStepResult, LogMarker } from './steps';
import Log from '../log';

type OutputSource = 'stdout' | 'stderr';

// One line of the build log, in the JSON shape the server and dashboard read.
export interface LogLine {
  time: string;
  level: 30 | 40 | 50;
  msg: string;
  buildStepId: string;
  buildStepDisplayName: BuildStep;
  source?: OutputSource;
  marker?: LogMarker;
  result?: BuildStepResult;
  durationMs?: number;
}

// Receives every line once streaming is on and sends it to the server.
export interface LogUploader {
  write(line: LogLine): void;
  close(): Promise<void>;
}

// Writes messages to an identified build step.
export interface LogWriter {
  // write keeps a line in the log only; info and warn also show it in the terminal.
  write(line: string, source?: OutputSource): void;
  info(line: string): void;
  warn(line: string): void;
}

export interface StepLogger extends LogWriter {
  markSkipped(): void;
}

// The log of a whole build: a local file, optionally streamed to the server.
export interface BuildLog {
  path: string;
  general: LogWriter;
  maskSecrets(secrets: string[]): void;
  streamTo(uploader: LogUploader): void;
  // Steps run sequentially; overlapping calls reject until the active work settles.
  runStep<T>(displayName: BuildStep, work: (stepLog: StepLogger) => Promise<T>): Promise<T>;
  abort(): void;
  close(): Promise<void>;
}

type StepIdentity = Pick<LogLine, 'buildStepId' | 'buildStepDisplayName'>;

// Lines written before streamTo is called are held for the uploader, up to this size.
const MAX_HELD_BYTES = 256 * 1024;

export async function createBuildLog(
  project: string,
  profile: string,
  verbose = false
): Promise<BuildLog> {
  const logFile = await openLogFile(project, profile);
  let redact = createBuildOutputRedactor([]);
  let uploader: LogUploader | undefined;
  const heldForUpload: LogLine[] = [];
  let heldBytes = 0;
  let closing: Promise<void> | undefined;
  let failCurrentStep: (() => void) | undefined;

  const addLine = (
    stepFields: StepIdentity,
    msg: string,
    extra: Partial<Omit<LogLine, keyof StepIdentity | 'time' | 'msg'>> = {}
  ): void => {
    if (closing) {
      return;
    }
    const logLine: LogLine = {
      time: new Date().toISOString(),
      level: 30,
      msg: redact(msg),
      ...stepFields,
      ...extra,
    };
    if (uploader) {
      uploader.write(logLine);
    } else if (heldBytes < MAX_HELD_BYTES) {
      heldForUpload.push(logLine);
      heldBytes += Buffer.byteLength(JSON.stringify(logLine));
    }
    logFile.append(`[${logLine.buildStepDisplayName}] ${logLine.msg}`);
  };

  const loggingMethods = (stepFields: StepIdentity, onWarning?: () => void): LogWriter => {
    const terminalLine = (line: string): string =>
      `[${stepFields.buildStepDisplayName}] ${redact(line)}`;
    return {
      write(line, source) {
        addLine(stepFields, line, { source });
        if (verbose) {
          (source === 'stderr' ? process.stderr : process.stdout).write(`${terminalLine(line)}\n`);
        }
      },
      info(line) {
        addLine(stepFields, line);
        Log.log(terminalLine(line));
      },
      warn(line) {
        onWarning?.();
        addLine(stepFields, line, { level: 40 });
        Log.warn(terminalLine(line));
      },
    };
  };

  return {
    path: logFile.path,
    general: loggingMethods({ buildStepId: randomUUID(), buildStepDisplayName: BuildStep.GENERAL }),
    maskSecrets(secrets) {
      redact = createBuildOutputRedactor(secrets);
    },
    streamTo(target) {
      uploader = target;
      for (const line of heldForUpload) {
        uploader.write({ ...line, msg: redact(line.msg) });
      }
      heldForUpload.length = 0;
    },
    async runStep(displayName, work) {
      if (failCurrentStep) {
        throw new Error('Cannot start a build step while another step is running.');
      }
      const stepFields: StepIdentity = {
        buildStepId: randomUUID(),
        buildStepDisplayName: displayName,
      };
      let warned = false;
      let skipped = false;
      const stepLog: StepLogger = {
        ...loggingMethods(stepFields, () => {
          warned = true;
        }),
        markSkipped() {
          skipped = true;
        },
      };

      const startedAt = Date.now();
      let finished = false;
      const finishStep = (result: BuildStepResult): void => {
        if (finished) {
          return;
        }
        finished = true;
        addLine(stepFields, `End step: ${displayName}`, {
          marker: LogMarker.END_STEP,
          result,
          durationMs: Date.now() - startedAt,
        });
        const summary = `${displayName} — ${result}`;
        if (result === BuildStepResult.FAIL) {
          Log.fail(summary);
        } else {
          Log.succeed(summary);
        }
      };

      failCurrentStep = (): void => {
        finishStep(BuildStepResult.FAIL);
      };
      try {
        addLine(stepFields, `Start step: ${displayName}`, { marker: LogMarker.START_STEP });
        Log.log(displayName);
        const value = await work(stepLog);
        finishStep(
          skipped
            ? BuildStepResult.SKIPPED
            : warned
              ? BuildStepResult.WARNING
              : BuildStepResult.SUCCESS
        );
        return value;
      } catch (error) {
        addLine(stepFields, error instanceof Error ? error.message : 'Build step failed.', {
          level: 50,
        });
        finishStep(BuildStepResult.FAIL);
        throw error;
      } finally {
        failCurrentStep = undefined;
      }
    },
    abort() {
      failCurrentStep?.();
    },
    close() {
      closing ??= (async () => {
        try {
          await logFile.close();
        } finally {
          await uploader?.close();
        }
      })();
      return closing;
    },
  };
}

// Opens the log for one build, records a failure in it and points the error at
// the file, and always closes it.
export async function withBuildLog<T>(
  project: string,
  profile: string,
  title: string,
  work: (buildLog: BuildLog) => Promise<T>,
  verbose = false
): Promise<T> {
  const buildLog = await createBuildLog(project, profile, verbose);
  buildLog.general.write(`${title} — profile ${profile} — ${new Date().toISOString()}`);
  Log.log(`Build log: ${buildLog.path}`);
  let result: T;
  try {
    result = await work(buildLog);
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Build failed.';
    buildLog.general.write(message);
    await buildLog.close().catch(closeError => {
      Log.warn(`Build log may be incomplete: ${(closeError as Error).message}`);
    });
    throw new Error(`${message}\n\nFull build log: ${buildLog.path}`, { cause: error });
  }
  await buildLog.close();
  return result;
}

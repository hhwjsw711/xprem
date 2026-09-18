import { ExpoConfig } from '@expo/config';
import fs from 'fs-extra';
import path from 'path';
import resolveFrom from 'resolve-from';

import { Workflow } from '../workflow';
import { LogWriter } from './log';
import { BuildInputs } from './prepare';
import { runBuildCommand } from './run';
import { BuildPlatform } from './server';
import { resolveRuntimeVersionAsync } from '../runtimeVersion';

export async function fingerprintBuild(
  build: BuildInputs,
  platform: BuildPlatform,
  working: string,
  temporary: string,
  expo: ExpoConfig,
  stepLog: LogWriter,
  secrets: string[]
): Promise<{ fingerprint: string; expoSdk: string; runtimeVersion?: string }> {
  const fingerprintModule =
    resolveFrom.silent(working, 'expo/fingerprint') ?? require.resolve('@expo/fingerprint');
  const output = path.join(temporary, 'fingerprint.json');
  await runBuildCommand(
    {
      title: 'Computing Expo fingerprint',
      command: process.execPath,
      args: [
        path.resolve(__dirname, '../../../templates/fingerprint.cjs'),
        working,
        fingerprintModule,
        output,
        platform,
      ],
      cwd: working,
      env: build.env,
    },
    stepLog,
    secrets
  );
  const fingerprint = await fs.readJson(output);
  if (
    !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(fingerprint.fingerprint) ||
    typeof fingerprint.expoSdk !== 'string'
  ) {
    throw new Error('Invalid Expo fingerprint result.');
  }
  const runtime = await resolveRuntimeVersionAsync({
    exp: expo,
    platform,
    workflow: (await fs.pathExists(path.join(working, platform)))
      ? Workflow.GENERIC
      : Workflow.MANAGED,
    projectDir: working,
    cwd: working,
    env: Object.fromEntries(
      Object.entries(build.env).filter((entry): entry is [string, string] => entry[1] !== undefined)
    ),
  });
  return {
    fingerprint: fingerprint.fingerprint,
    expoSdk: fingerprint.expoSdk,
    runtimeVersion: runtime?.runtimeVersion ?? undefined,
  };
}

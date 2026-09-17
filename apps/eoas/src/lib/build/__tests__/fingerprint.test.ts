import fs from 'fs-extra';
import os from 'os';
import path from 'path';
import { afterEach, beforeEach, expect, it } from 'vitest';

import { createBuildLog } from '../log';
import { BuildPhase } from '../phases';
import { runBuildCommand } from '../run';

let project: string;
let fingerprintModule: string;
beforeEach(async () => {
  project = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-fingerprint-'));
  fingerprintModule = path.join(project, 'fingerprint.cjs');
  await fs.writeFile(
    fingerprintModule,
    "exports.createFingerprintAsync = async () => ({ hash: 'abc123' });"
  );
  await fs.outputJson(path.join(project, 'node_modules/expo/package.json'), { version: '52.0.0' });
});
afterEach(async () => {
  await fs.remove(project);
});

it.each([
  ['fingerprint', 'Fingerprint failed'],
  ['dependency', "Cannot find module 'expo/package.json'"],
  ['output', 'ENOENT'],
  ['non-Error rejection', 'Fingerprint rejected'],
])('preserves %s failure details and a nonzero exit in build logs', async (failure, detail) => {
  let output = path.join(project, 'fingerprint.json');
  if (failure === 'fingerprint') {
    await fs.writeFile(
      fingerprintModule,
      "exports.createFingerprintAsync = async () => { throw new Error('Fingerprint failed'); };"
    );
  } else if (failure === 'dependency') {
    await fs.remove(path.join(project, 'node_modules/expo'));
  } else if (failure === 'output') {
    output = path.join(project, 'missing', 'fingerprint.json');
  } else {
    await fs.writeFile(
      fingerprintModule,
      "exports.createFingerprintAsync = async () => { throw 'Fingerprint rejected'; };"
    );
  }
  const log = await createBuildLog(project, 'test');
  try {
    await expect(
      log.runBuildPhase(BuildPhase.CALCULATE_EXPO_UPDATES_RUNTIME_VERSION, phase =>
        runBuildCommand(
          {
            title: 'Computing Expo fingerprint',
            command: process.execPath,
            args: [
              path.resolve(__dirname, '../../../../templates/fingerprint.cjs'),
              project,
              fingerprintModule,
              output,
              'android',
            ],
            cwd: project,
            env: process.env,
          },
          phase,
          []
        )
      )
    ).rejects.toThrow(/exit code 1/);
  } finally {
    await log.close();
  }
  const contents = await fs.readFile(log.path, 'utf8');
  expect(contents).toContain('Check the project configuration and installed dependencies.');
  expect(contents).toContain(detail);
});

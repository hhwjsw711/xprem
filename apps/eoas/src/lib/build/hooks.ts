import { resolvePackageManager } from '@expo/package-manager';
import fs from 'fs-extra';
import path from 'path';

import { BuildLog } from './log';
import { BuildInputs } from './prepare';
import { runBuildCommand } from './run';
import { BuildStep } from './steps';

const POST_INSTALL = 'eoas-build-post-install';

// Runs the project's post-install script in the build copy, once native dependencies are in place
// and before the native build.
export async function runPostInstallHook(
  build: BuildInputs,
  working: string,
  buildLog: BuildLog,
  secrets: string[]
): Promise<void> {
  await buildLog.runStep(BuildStep.POST_INSTALL_HOOK, async stepLog => {
    const scripts: Record<string, string> =
      (await fs.readJson(path.join(working, 'package.json')).catch(() => ({}))).scripts ?? {};
    if (!scripts[POST_INSTALL]) {
      if (scripts['eas-build-post-install']) {
        stepLog.info(
          `package.json has an "eas-build-post-install" script. Name it "${POST_INSTALL}" to run it in this build.`
        );
      }
      stepLog.markSkipped();
      return;
    }
    await runBuildCommand(
      {
        title: `Running ${POST_INSTALL}`,
        command: resolvePackageManager(working) ?? 'npm',
        args: ['run', POST_INSTALL],
        cwd: working,
        env: build.env,
      },
      stepLog,
      secrets
    );
  });
}

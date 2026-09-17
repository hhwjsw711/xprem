import fs from 'fs-extra';
import os from 'os';
import path from 'path';
import { afterEach, expect, it } from 'vitest';

import { assertSceneLifecycle } from '../sceneLifecycle';

const directories: string[] = [];
afterEach(async () => {
  await Promise.all(directories.splice(0).map(directory => fs.remove(directory)));
});

async function project(infoPlist: string, appDelegate = ''): Promise<string> {
  const working = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-scene-'));
  directories.push(working);
  await fs.outputFile(path.join(working, 'ios/app/Info.plist'), infoPlist);
  await fs.outputFile(path.join(working, 'ios/app/AppDelegate.swift'), appDelegate);
  return working;
}

it('stops an Xcode 27 build of a project that has not adopted scenes', async () => {
  const legacy = await project('<plist><dict><key>CFBundleName</key></dict></plist>');
  await expect(assertSceneLifecycle(legacy, 27)).rejects.toThrow('UIApplicationSceneManifest');
  await expect(assertSceneLifecycle(legacy, 26)).resolves.toBeUndefined();

  const declared = await project(
    '<plist><dict><key>UIApplicationSceneManifest</key></dict></plist>'
  );
  await expect(assertSceneLifecycle(declared, 27)).resolves.toBeUndefined();

  const delegated = await project(
    '<plist/>',
    'func application(_ a: UIApplication, configurationForConnecting s: UISceneSession) {}'
  );
  await expect(assertSceneLifecycle(delegated, 27)).resolves.toBeUndefined();
});

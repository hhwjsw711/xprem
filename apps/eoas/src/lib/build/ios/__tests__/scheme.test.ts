import fs from 'fs-extra';
import os from 'os';
import path from 'path';
import { afterEach, expect, it } from 'vitest';

import { appScheme } from '../index';

const directories: string[] = [];
afterEach(async () => {
  await Promise.all(directories.splice(0).map(directory => fs.remove(directory)));
});

async function projectWithSchemes(...schemes: string[]): Promise<string> {
  const project = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-scheme-'));
  directories.push(project);
  const shared = path.join(project, 'ios/App.xcodeproj/xcshareddata/xcschemes');
  await fs.ensureDir(shared);
  await Promise.all(
    schemes.map(scheme => fs.writeFile(path.join(shared, `${scheme}.xcscheme`), ''))
  );
  return project;
}

it('builds the only shared scheme of the project', async () => {
  expect(appScheme(await projectWithSchemes('MyApp'))).toBe('MyApp');
});

it('builds the scheme of the profile when the project has several', async () => {
  const project = await projectWithSchemes('MyApp-Prod', 'MyApp-Staging');
  expect(appScheme(project, 'MyApp-Staging')).toBe('MyApp-Staging');
  expect(() => appScheme(project)).toThrow('Set "ios.scheme"');
  expect(() => appScheme(project, 'MyApp')).toThrow('no shared scheme "MyApp"');
});

it('explains that a project without a shared scheme cannot be built', async () => {
  const project = await projectWithSchemes();
  expect(() => appScheme(project)).toThrow('Shared');
});

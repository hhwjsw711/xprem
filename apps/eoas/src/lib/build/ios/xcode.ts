import spawnAsync from '@expo/spawn-async';
import fg from 'fast-glob';
import fs from 'fs-extra';
import path from 'path';

import Log from '../../log';
import { selectAsync } from '../../prompts';

export interface InstalledXcode {
  version: string;
  developerDir: string;
}

const VERSION_FILE = '.xcode-version';

// Picks the Xcode of one build: the requested version, then .xcode-version, then a question when
// several are installed. Returns undefined to keep the Xcode that xcode-select points at.
export async function selectXcode(
  project: string,
  requested?: string
): Promise<string | undefined> {
  const installed = await installedXcodes();
  const wanted = requested ?? (await readVersionFile(project));
  if (wanted) {
    const match = matchXcode(installed, wanted);
    if (!match) {
      throw new Error(
        `Xcode ${wanted} is not installed. Installed: ${
          installed.map(xcode => xcode.version).join(', ') || 'none found in /Applications'
        }.`
      );
    }
    return match.developerDir;
  }
  if (installed.length < 2 || !process.stdin.isTTY || !process.stdout.isTTY) {
    return undefined;
  }
  const current = await selectedDeveloperDir();
  const developerDir = await selectAsync(
    'Which Xcode builds this app?',
    installed.map(xcode => ({
      title: `Xcode ${xcode.version}${xcode.developerDir === current ? ' (default)' : ''}`,
      value: xcode.developerDir,
    })),
    { initial: current }
  );
  Log.log(`Write the version to ${VERSION_FILE} or pass --xcode to skip this question.`);
  return developerDir;
}

// The newest installed Xcode whose version starts with the requested components: "26" matches 26.1.
export function matchXcode(
  installed: InstalledXcode[],
  requested: string
): InstalledXcode | undefined {
  const wanted = requested.trim().split('.');
  return installed.find(xcode =>
    wanted.every((component, index) => xcode.version.split('.')[index] === component)
  );
}

// Xcode apps of /Applications and the one xcode-select points at, newest first.
async function installedXcodes(): Promise<InstalledXcode[]> {
  const developerDirs = new Set(
    (await fg('/Applications/Xcode*.app', { onlyDirectories: true })).map(app =>
      path.join(app, 'Contents/Developer')
    )
  );
  const current = await selectedDeveloperDir();
  if (current?.endsWith('.app/Contents/Developer')) {
    developerDirs.add(current);
  }
  const installed: InstalledXcode[] = [];
  for (const developerDir of developerDirs) {
    try {
      const { stdout } = await spawnAsync('/usr/libexec/PlistBuddy', [
        '-c',
        'Print :CFBundleShortVersionString',
        path.join(developerDir, '../version.plist'),
      ]);
      installed.push({ version: stdout.trim(), developerDir });
    } catch {
      // An app named Xcode* that is not an Xcode install.
    }
  }
  return installed.sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }));
}

async function selectedDeveloperDir(): Promise<string | undefined> {
  try {
    return (await spawnAsync('xcode-select', ['-p'])).stdout.trim();
  } catch {
    return undefined;
  }
}

async function readVersionFile(project: string): Promise<string | undefined> {
  const file = path.join(project, VERSION_FILE);
  return (await fs.pathExists(file)) ? (await fs.readFile(file, 'utf8')).trim() : undefined;
}

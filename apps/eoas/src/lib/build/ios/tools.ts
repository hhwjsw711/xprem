import spawnAsync from '@expo/spawn-async';
import os from 'os';

// Checks Xcode and CocoaPods and returns the Xcode major version; developerDir picks another Xcode
// than the one xcode-select points at.
export async function resolveIosTools(
  report: (message: string) => void,
  developerDir?: string
): Promise<number> {
  if (os.platform() !== 'darwin') {
    throw new Error('Local iOS builds require macOS with Xcode.');
  }
  const xcode = await toolVersion(
    'xcodebuild',
    ['-version'],
    'Xcode was not found. Install Xcode, then run "sudo xcode-select -s /Applications/Xcode.app".',
    developerDir
  );
  const major = Number(/^Xcode (\d+)/.exec(xcode)?.[1]);
  if (!major) {
    throw new Error('Could not read the Xcode version from "xcodebuild -version".');
  }
  report(xcode.split('\n')[0]);
  const pods = await toolVersion(
    'pod',
    ['--version'],
    'CocoaPods was not found. Install it with "brew install cocoapods".'
  );
  report(`CocoaPods ${pods}`);
  return major;
}

// The macOS SDK of the chosen Xcode. A bare clang run by a pod script otherwise links against the
// Command Line Tools SDK, which an older Xcode cannot read.
export async function macosSdkPath(developerDir: string): Promise<string> {
  const { stdout } = await spawnAsync('xcrun', ['--sdk', 'macosx', '--show-sdk-path'], {
    env: { ...process.env, DEVELOPER_DIR: developerDir },
  });
  return stdout.trim();
}

async function toolVersion(
  command: string,
  args: string[],
  missing: string,
  developerDir?: string
): Promise<string> {
  try {
    const env = developerDir ? { ...process.env, DEVELOPER_DIR: developerDir } : process.env;
    return (await spawnAsync(command, args, { env })).stdout.trim();
  } catch {
    throw new Error(missing);
  }
}

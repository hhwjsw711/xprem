import fg from 'fast-glob';
import fs from 'fs-extra';

const FIRST_ENFORCING_XCODE = 27;

// iOS refuses to launch an app built with the iOS 27 SDK that has not adopted the UIKit scene life
// cycle, so such a build is stopped before it is compiled.
export async function assertSceneLifecycle(working: string, xcodeMajor: number): Promise<void> {
  if (xcodeMajor < FIRST_ENFORCING_XCODE) {
    return;
  }
  const files = await fg(['ios/*/Info.plist', 'ios/*/**/*.{swift,m,mm}'], {
    cwd: working,
    absolute: true,
    ignore: ['ios/Pods/**', 'ios/build/**'],
  });
  for (const file of files) {
    const contents = await fs.readFile(file, 'utf8');
    if (/UIApplicationSceneManifest|configurationForConnecting|WindowGroup/.test(contents)) {
      return;
    }
  }
  throw new Error(
    `Xcode ${xcodeMajor} builds with the iOS ${xcodeMajor} SDK, and iOS ${xcodeMajor} closes at launch any app built with it that has not adopted the UIKit scene life cycle. This project does not: its Info.plist has no UIApplicationSceneManifest, which means its Expo SDK predates Xcode ${xcodeMajor}.\n\nInstall the Xcode your Expo SDK supports next to this one and pick it with --xcode, for instance --xcode ${
      xcodeMajor - 1
    }, or upgrade to an Expo SDK that supports Xcode ${xcodeMajor}.`
  );
}

import { IOSConfig } from '@expo/config-plugins';
import spawnAsync from '@expo/spawn-async';
import fg from 'fast-glob';
import fs from 'fs-extra';
import path from 'path';

import { assertSceneLifecycle } from './sceneLifecycle';
import { InstalledSigning, IosCredentials, installSigning } from './signing';
import { macosSdkPath, resolveIosTools } from './tools';
import { selectXcode } from './xcode';
import { IosProfile } from '../../buildConfig/types';
import Log from '../../log';
import { secretsToRedact } from '../errors';
import { BuildLog, withBuildLog } from '../log';
import { NativeBuild, NativeWorkspace, prepareNativeBuild, runNativeBuild } from '../native';
import { BuildPhase } from '../phases';
import { BuildInputs, BuildOptions, platformProfile } from '../prepare';
import { runBuildCommand } from '../run';
import { fetchCredentials } from '../server';
import { withTemporaryDirectory } from '../workspace';

interface IosBuild extends BuildInputs {
  credentials: IosCredentials;
  ios: IosProfile;
  xcodeMajor: number;
}

export type IosBuildOptions = BuildOptions & { xcode?: string };

export async function buildIos(project: string, options: IosBuildOptions): Promise<string> {
  const developerDir = await selectXcode(project, options.xcode);
  return await withBuildLog(
    project,
    options.profile,
    'iOS build',
    async buildLog => {
      const build = await prepareBuild(project, options, buildLog, developerDir);
      const secrets = secretsToRedact(build.variables, [
        build.credentials.certificateP12,
        build.credentials.certificatePassword,
      ]);
      buildLog.maskSecrets(secrets);
      return await withTemporaryDirectory(
        buildLog,
        temporary => runNativeBuild(build, iosBuild(build), temporary, buildLog, secrets),
        project
      );
    },
    options.verbose || Log.isDebug
  );
}

async function prepareBuild(
  project: string,
  options: BuildOptions,
  buildLog: BuildLog,
  developerDir?: string
): Promise<IosBuild> {
  let xcodeMajor = 0;
  const inputs = await prepareNativeBuild<IosCredentials>(project, options, buildLog, {
    platform: 'ios',
    toolsTitle: 'Check local iOS tools',
    credentialsTitle: 'Prepare iOS signing credentials',
    resolveTools: async (_local, phaseLog) => {
      xcodeMajor = await resolveIosTools(message => {
        phaseLog.info(message);
      }, developerDir);
      // CocoaPods refuses to run without a UTF-8 locale.
      return { LANG: 'en_US.UTF-8', ...(developerDir ? { DEVELOPER_DIR: developerDir } : {}) };
    },
    describe: profile => {
      const { bundleIdentifier, developmentClient } = platformProfile(profile, 'ios');
      return {
        applicationId: bundleIdentifier,
        mode: developmentClient ? 'debug' : 'release',
        extension: 'ipa',
      };
    },
    fetchCredentials: (endpoint, profile) =>
      fetchCredentials<IosCredentials>(
        endpoint,
        'ios',
        ['certificateP12', 'certificatePassword', 'provisioningProfile', 'teamId'],
        { distribution: platformProfile(profile, 'ios').distribution }
      ),
  });
  return { ...inputs, ios: platformProfile(inputs.profile, 'ios'), xcodeMajor };
}

function iosBuild(build: IosBuild): NativeBuild {
  const { bundleIdentifier, developmentClient, distribution } = build.ios;
  const configuration = build.ios.buildConfiguration ?? (developmentClient ? 'Debug' : 'Release');
  return {
    platform: 'ios',
    displayName: 'iOS',
    artifactType: 'ipa',
    mode: developmentClient ? 'debug' : 'release',
    distribution,
    developmentClient,
    buildNumberName: 'build number',
    maintainedProjectNotice:
      'Using maintained iOS project. Native settings are retained; bundle identifier, build number, signing and the expo-updates configuration are overridden in the temporary copy.',
    withIdentity: (expo, buildNumber) => ({
      ...expo,
      ios: { ...expo.ios, bundleIdentifier, buildNumber: String(buildNumber) },
    }),
    compile: async workspace => {
      const { working, temporary, buildLog, secrets } = workspace;
      let signing: InstalledSigning | undefined;
      try {
        const { scheme, installed } = await buildLog.runBuildPhase(
          BuildPhase.CONFIGURE_XCODE_PROJECT,
          async () => {
            await assertSceneLifecycle(working, build.xcodeMajor);
            const scheme = appScheme(working, build.ios.scheme);
            await assertDeviceDestination(working, scheme, build.env);
            signing = await installSigning(build.credentials, temporary);
            await configureXcodeProject(build, workspace, signing, configuration, scheme);
            return { scheme, installed: signing };
          }
        );
        await buildLog.runBuildPhase(BuildPhase.INSTALL_PODS, async phaseLog => {
          const developerDir = build.toolEnv.DEVELOPER_DIR;
          await runBuildCommand(
            {
              title: 'Installing pods',
              command: 'pod',
              args: ['install'],
              silence: { warnAfterMs: 15 * 60 * 1000, stopAfterMs: 30 * 60 * 1000 },
              cwd: path.join(working, 'ios'),
              env: developerDir
                ? { ...build.env, SDKROOT: await macosSdkPath(developerDir) }
                : build.env,
            },
            phaseLog,
            secrets
          );
        });
        await buildLog.runBuildPhase(
          BuildPhase.RUN_XCODEBUILD,
          async phaseLog => {
            const archive = path.join(temporary, 'app.xcarchive');
            const exportOptions = path.join(temporary, 'exportOptions.plist');
            await fs.writeFile(exportOptions, exportOptionsPlist(build, installed.profileUuid));
            const [workspaceFile] = await fg('ios/*.xcworkspace', {
              cwd: working,
              absolute: true,
              onlyDirectories: true,
            });
            if (!workspaceFile) {
              throw new Error('No Xcode workspace was found in ios/ after installing pods.');
            }
            const rawOutput = buildLog.path.replace(/\.log$/, '.xcodebuild.log');
            phaseLog.info(`Complete Xcode output: ${rawOutput}`);
            const { summarize, close } = xcodeOutputSummary(rawOutput, message => {
              phaseLog.warn(message);
            });
            const xcodebuild = (title: string, args: string[]): Promise<void> =>
              runBuildCommand(
                {
                  title,
                  command: 'xcodebuild',
                  args: ['-hideShellScriptEnvironment', ...args],
                  cwd: path.join(working, 'ios'),
                  env: build.env,
                  transform: summarize,
                },
                phaseLog,
                secrets
              );
            try {
              await xcodebuild('Archiving the app', [
                '-workspace',
                workspaceFile,
                '-scheme',
                scheme,
                '-configuration',
                configuration,
                '-destination',
                'generic/platform=iOS',
                '-archivePath',
                archive,
                '-derivedDataPath',
                path.join(temporary, 'DerivedData'),
                'archive',
              ]);
              await xcodebuild('Exporting the signed IPA', [
                '-exportArchive',
                '-archivePath',
                archive,
                '-exportPath',
                path.join(temporary, 'export'),
                '-exportOptionsPlist',
                exportOptions,
                `OTHER_CODE_SIGN_FLAGS=--keychain ${installed.keychain}`,
              ]);
            } finally {
              await close();
            }
          },
          'Building signed IPA'
        );
      } finally {
        signing?.remove();
      }
    },
    findArtifact: async ({ temporary }) => {
      const ipas = await fg('export/*.ipa', { cwd: temporary, absolute: true });
      if (ipas.length !== 1) {
        throw new Error('Expected one IPA in the Xcode export.');
      }
      return ipas[0];
    },
  };
}

// Xcode prints megabytes per build: the complete output goes to a file beside the build log, which
// only receives one line per target, the errors and the result banner.
function xcodeOutputSummary(
  rawPath: string,
  warn: (message: string) => void
): { summarize: (line: string) => string | undefined; close: () => Promise<void> } {
  let raw: fs.WriteStream | undefined = fs.createWriteStream(rawPath, { flags: 'a', mode: 0o600 });
  raw.on('error', () => {
    raw = undefined;
    warn(`Could not write the complete Xcode output to ${rawPath}; the build continues.`);
  });
  const targets = new Set<string>();
  return {
    summarize: line => {
      raw?.write(`${line}\n`);
      if (/(^|\s)(fatal )?error: |^\*\* [A-Z ]+ \*\*$|^ld: |^\[[\w-]+\] /.test(line)) {
        return line;
      }
      const target = /\(in target '([^']+)' from project '[^']+'\)$/.exec(line)?.[1];
      if (target && !targets.has(target)) {
        targets.add(target);
        return `› Building ${target}`;
      }
      return undefined;
    },
    close: () =>
      new Promise(resolve => {
        if (raw) {
          raw.end(() => {
            resolve();
          });
        } else {
          resolve();
        }
      }),
  };
}

// The shared scheme to build: the one of the profile, or the only one of the project.
export function appScheme(working: string, configured?: string): string {
  const schemes = IOSConfig.BuildScheme.getSchemesFromXcodeproj(working);
  if (schemes.length === 0) {
    throw new Error(
      'No scheme was found in the Xcode project. Mark at least one scheme as "Shared" in Xcode, so that "xcodebuild -list" prints it.'
    );
  }
  const found = schemes.join(', ');
  if (configured) {
    if (!schemes.includes(configured)) {
      throw new Error(`The Xcode project has no shared scheme "${configured}". Found: ${found}.`);
    }
    return configured;
  }
  if (schemes.length > 1) {
    throw new Error(
      `The Xcode project has several schemes: ${found}. Set "ios.scheme" on this profile in xprem.json.`
    );
  }
  return schemes[0];
}

// Asks Xcode whether it can build for a device; a missing iOS platform otherwise fails after the pods.
async function assertDeviceDestination(
  working: string,
  scheme: string,
  env: NodeJS.ProcessEnv
): Promise<void> {
  const [project] = await fg('ios/*.xcodeproj', {
    cwd: working,
    absolute: true,
    onlyDirectories: true,
    ignore: ['ios/Pods/**'],
  });
  const { stdout } = await spawnAsync(
    'xcodebuild',
    ['-project', project, '-scheme', scheme, '-showdestinations'],
    { env }
  ).catch(error => ({ stdout: String((error as { stdout?: string }).stdout ?? '') }));
  const refusal = /name:Any iOS Device, error:([^}]*)}/.exec(stdout)?.[1]?.trim();
  if (refusal) {
    throw new Error(
      `${refusal}\n\nFrom a terminal: ${
        env.DEVELOPER_DIR ? `DEVELOPER_DIR=${env.DEVELOPER_DIR} ` : ''
      }xcodebuild -downloadPlatform iOS`
    );
  }
}

// Points the app target at the profile's identifier, build number and manual signing.
async function configureXcodeProject(
  build: IosBuild,
  { working, buildNumber }: NativeWorkspace,
  signing: InstalledSigning,
  configuration: string,
  scheme: string
): Promise<void> {
  const { targetName, infoPlist } = await configureAppTarget(
    working,
    scheme,
    configuration,
    build.ios.bundleIdentifier
  );
  await setPlistString(infoPlist, 'CFBundleVersion', String(buildNumber));
  IOSConfig.ProvisioningProfile.setProvisioningProfileForPbxproj(working, {
    targetName,
    profileName: signing.profileName,
    appleTeamId: signing.teamId,
    buildConfiguration: configuration,
  });
}

// Sets the bundle identifier on the app target the scheme builds and returns its Info.plist.
export async function configureAppTarget(
  working: string,
  scheme: string,
  configuration: string,
  bundleIdentifier: string
): Promise<{ targetName: string; infoPlist: string }> {
  const application = await IOSConfig.Target.findApplicationTargetWithDependenciesAsync(
    working,
    scheme
  );
  const extensions = signableDependencies(application);
  if (extensions.length > 0) {
    throw new Error(
      `This app has extensions (${extensions.join(
        ', '
      )}). Each one needs its own provisioning profile, which is not supported yet.`
    );
  }
  const project = IOSConfig.XcodeUtils.getPbxproj(working);
  const [, target] = IOSConfig.Target.findNativeTargetByName(project, application.name);
  const configurations = IOSConfig.XcodeUtils.getBuildConfigurationsForListId(
    project,
    target.buildConfigurationList
  );
  const names = configurations.map(([, item]) => unquoted(item.name));
  const selected = configurations.find(([, item]) => unquoted(item.name) === configuration);
  if (!selected) {
    throw new Error(
      `Target "${
        application.name
      }" has no build configuration "${configuration}". Found: ${names.join(', ')}.`
    );
  }
  for (const [, item] of configurations) {
    item.buildSettings.PRODUCT_BUNDLE_IDENTIFIER = `"${bundleIdentifier}"`;
  }
  // eslint-disable-next-line node/no-sync
  await fs.writeFile(project.filepath, project.writeSync());
  return {
    targetName: application.name,
    infoPlist:
      targetInfoPlist(working, selected[1].buildSettings.INFOPLIST_FILE) ??
      IOSConfig.Paths.getInfoPlistPath(working),
  };
}

function signableDependencies(target: IOSConfig.Target.Target): string[] {
  return (target.dependencies ?? []).flatMap(dependency => [
    ...(dependency.signable ? [dependency.name] : []),
    ...signableDependencies(dependency),
  ]);
}

function unquoted(value: string): string {
  return value.replace(/^"|"$/g, '');
}

// The file INFOPLIST_FILE names, or undefined when it depends on a setting that is not a directory of the project.
export function targetInfoPlist(working: string, setting?: string): string | undefined {
  if (!setting) {
    return undefined;
  }
  const ios = path.join(working, 'ios');
  const resolved = unquoted(setting).replace(/\$[({](SRCROOT|PROJECT_DIR)[)}]/g, ios);
  return resolved.includes('$') ? undefined : path.resolve(ios, resolved);
}

async function setPlistString(file: string, key: string, value: string): Promise<void> {
  const plistBuddy = (command: string): Promise<unknown> =>
    spawnAsync('/usr/libexec/PlistBuddy', ['-c', command, file]);
  try {
    await plistBuddy(`Set :${key} ${value}`);
  } catch {
    await plistBuddy(`Add :${key} string ${value}`);
  }
}

// Xcode 15.3 renamed the export methods, so 16 is the first major sure to know the new names.
function exportOptionsPlist(build: IosBuild, profileUuid: string): string {
  const renamed = build.xcodeMajor >= 16;
  const method =
    build.ios.distribution === 'app-store'
      ? renamed
        ? 'app-store-connect'
        : 'app-store'
      : renamed
        ? 'release-testing'
        : 'ad-hoc';
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>method</key>
  <string>${method}</string>
  <key>teamID</key>
  <string>${build.credentials.teamId}</string>
  <key>signingStyle</key>
  <string>manual</string>
  <key>provisioningProfiles</key>
  <dict>
    <key>${build.ios.bundleIdentifier}</key>
    <string>${profileUuid}</string>
  </dict>
</dict>
</plist>
`;
}

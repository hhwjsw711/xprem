import { ExpoConfig } from '@expo/config';
import fs from 'fs-extra';
import path from 'path';

import {
  ArtifactType,
  failBuildRecord,
  finishBuildRecord,
  startBuildRecord,
  uploadBuildArtifact,
} from './artifacts';
import { secretsToRedact } from './errors';
import { fingerprintBuild } from './fingerprint';
import { BuildLog, PhaseLogger } from './log';
import { BuildPhase } from './phases';
import {
  BuildInputs,
  BuildOptions,
  configEnvironment,
  fetchBuildEnvironment,
  nodeEnvFor,
  readEnvFile,
  resolveBuildEndpoint,
  resolveOutputPath,
  selectProfile,
  spawnEnvironment,
} from './prepare';
import { runBuildCommand } from './run';
import { BuildPlatform, allocateBuildNumber } from './server';
import { createLogUploader } from './upload';
import {
  copyProject,
  evaluateExpoConfig,
  expoCommand,
  installMetroCheck,
  restoreMetroConfig,
  validateBundle,
  writeAppJson,
} from './workspace';
import { BuildProfile } from '../buildConfig/types';
import Log from '../log';
import { resolvePackageRunner, splitPackageRunner } from '../packageRunner';
import { resolveExpoUpdatesCli } from '../runtimeVersion';

// What one platform resolves before the project is copied; everything else is shared.
export interface NativePreparation<C> {
  platform: BuildPlatform;
  toolsTitle: string;
  credentialsTitle: string;
  resolveTools(
    local: Record<string, string>,
    phaseLog: PhaseLogger,
    recordTool: (name: string, version: string) => void
  ): Promise<Record<string, string>>;
  describe(profile: BuildProfile): {
    applicationId: string;
    mode: 'debug' | 'release';
    extension: string;
  };
  fetchCredentials(endpoint: string, profile: BuildProfile): Promise<C>;
}

export async function prepareNativeBuild<C>(
  project: string,
  options: BuildOptions,
  buildLog: BuildLog,
  preparation: NativePreparation<C>
): Promise<BuildInputs & { credentials: C }> {
  const profile = await buildLog.runBuildPhase(
    BuildPhase.CUSTOM,
    () => selectProfile(project, options),
    'Read xprem.json'
  );
  const { applicationId, mode, extension } = preparation.describe(profile);
  const local = await readEnvFile(project, options.envFile);
  buildLog.maskSecrets(secretsToRedact(local, []));
  const toolVersions: Record<string, string> = {};
  const toolEnv = await buildLog.runBuildPhase(
    BuildPhase.BUILDER_INFO,
    phaseLog =>
      preparation.resolveTools(local, phaseLog, (name, version) => {
        toolVersions[name] = version;
      }),
    preparation.toolsTitle
  );
  const packageRunner = splitPackageRunner(resolvePackageRunner(options.packageRunner, project));
  const nodeEnv = nodeEnvFor(mode);
  const endpoint = await resolveBuildEndpoint(
    project,
    options,
    preparation.platform,
    applicationId,
    configEnvironment(local, toolEnv, nodeEnv)
  );
  const variables = await buildLog.runBuildPhase(BuildPhase.SET_UP_BUILD_ENVIRONMENT, phaseLog =>
    fetchBuildEnvironment(endpoint, profile, local, phaseLog)
  );
  buildLog.maskSecrets(secretsToRedact(variables, []));
  const credentials = await buildLog.runBuildPhase(
    BuildPhase.PREPARE_CREDENTIALS,
    () => preparation.fetchCredentials(endpoint, profile),
    preparation.credentialsTitle
  );
  const output = await resolveOutputPath(project, options, extension, buildLog);
  return {
    project,
    options,
    profile,
    credentials,
    packageRunner,
    endpoint,
    variables,
    output,
    nodeEnv,
    toolEnv,
    toolVersions,
    env: spawnEnvironment(variables, toolEnv, nodeEnv),
  };
}

export interface NativeWorkspace {
  working: string;
  temporary: string;
  buildNumber: number;
  buildLog: BuildLog;
  secrets: string[];
}

// What one platform contributes to a local build; everything else is shared.
export interface NativeBuild {
  platform: BuildPlatform;
  displayName: string;
  artifactType: ArtifactType;
  mode: 'debug' | 'release';
  distribution?: 'app-store' | 'ad-hoc';
  developmentClient?: boolean;
  buildNumberName: string;
  maintainedProjectNotice: string;
  // App config carrying the profile's identifier and the allocated build number.
  withIdentity(expo: ExpoConfig, buildNumber: number): ExpoConfig;
  // Signs and compiles the generated native project.
  compile(workspace: NativeWorkspace): Promise<void>;
  // Path of the compiled artifact inside the workspace.
  findArtifact(workspace: NativeWorkspace): Promise<string>;
}

export async function runNativeBuild(
  build: BuildInputs,
  native: NativeBuild,
  temporary: string,
  buildLog: BuildLog,
  secrets: string[]
): Promise<string> {
  const startedAt = new Date().toISOString();
  const { platform, mode } = native;
  const record = await startBuildRecord(
    build,
    native.artifactType,
    mode,
    startedAt,
    native.distribution
  );
  if (build.options.stream) {
    buildLog.streamTo(createLogUploader(build.endpoint, record.id, secrets, buildLog.warn));
    buildLog.info('Streaming build logs to xprem.');
  }
  buildLog.info(`Build ID: ${record.id}`);
  let output: string;
  try {
    const working = await buildLog.runBuildPhase(BuildPhase.PREPARE_PROJECT, async () => {
      const working = await copyProject(build.project, temporary);
      if (
        native.developmentClient &&
        !(await fs.pathExists(path.join(working, 'node_modules/expo-dev-client')))
      ) {
        throw new Error('This profile requires expo-dev-client to be installed.');
      }
      return working;
    });
    const expo = await buildLog.runBuildPhase(BuildPhase.READ_APP_CONFIG, async () => {
      const config = await evaluateExpoConfig(build, working);
      await writeAppJson(working, config);
      return config;
    });
    await buildLog.runBuildPhase(
      BuildPhase.EAGER_BUNDLE,
      async phaseLog => {
        await validateBundle(build, platform, mode, working, temporary, phaseLog, secrets);
        await restoreMetroConfig(working);
      },
      `Validate ${native.displayName} bundle`
    );
    const buildNumber = await buildLog.runBuildPhase(
      BuildPhase.CUSTOM,
      async () => Number(await allocateBuildNumber(build.endpoint)),
      'Allocate build number'
    );
    const effectiveExpo = native.withIdentity(expo, buildNumber);
    await writeAppJson(working, effectiveExpo);
    Object.assign(record.metadata, {
      version: expo.version,
      buildNumber: String(buildNumber),
      ...(await buildLog.runBuildPhase(
        BuildPhase.CALCULATE_EXPO_UPDATES_RUNTIME_VERSION,
        phaseLog =>
          fingerprintBuild(build, platform, working, temporary, effectiveExpo, phaseLog, secrets),
        'Calculate Expo fingerprint and runtime'
      )),
    });
    if (!build.options.ignoreEnvCheck) {
      await installMetroCheck(working, temporary, Object.keys(build.variables));
    }
    await buildLog.runBuildPhase(BuildPhase.PREBUILD, async phaseLog => {
      if (await fs.pathExists(path.join(working, platform))) {
        phaseLog.info(native.maintainedProjectNotice);
        // Same as EAS for bare projects: the manifest keeps whatever environment
        // the last prebuild saw, so channel, URL and runtime are re-synced here.
        await runBuildCommand(
          {
            title: 'Syncing expo-updates configuration',
            command: process.execPath,
            args: [
              resolveExpoUpdatesCli(working),
              'configuration:syncnative',
              '--platform',
              platform,
              '--workflow',
              'generic',
            ],
            cwd: working,
            env: build.env,
          },
          phaseLog,
          secrets
        );
      } else {
        await runBuildCommand(
          expoCommand(build, working, `Generating ${native.displayName} project`, [
            'prebuild',
            '--platform',
            platform,
            '--no-install',
          ]),
          phaseLog,
          secrets
        );
      }
    });
    const workspace: NativeWorkspace = { working, temporary, buildNumber, buildLog, secrets };
    await native.compile(workspace);
    output = await buildLog.runBuildPhase(BuildPhase.PREPARE_ARTIFACTS, async phaseLog => {
      await fs.ensureDir(path.dirname(build.output));
      await fs.copyFile(
        await native.findArtifact(workspace),
        build.output,
        fs.constants.COPYFILE_EXCL
      );
      const summary = `Built ${build.output} (${native.buildNumberName} ${buildNumber}).`;
      phaseLog.write(summary);
      Log.succeed(summary);
      await finishBuildRecord(record, build.output);
      return build.output;
    });
  } catch (error) {
    await failBuildRecord(record, build.output).catch(() => {
      buildLog.warn(
        'Could not report the failed build to the server. Local build metadata is retained.'
      );
    });
    throw error;
  }
  try {
    await uploadBuildArtifact(output, build.endpoint, buildLog);
  } catch (error) {
    await failBuildRecord(record, build.output).catch(() => {
      buildLog.warn('Could not report the failed upload to the server.');
    });
    const message = error instanceof Error ? error.message : 'Artifact upload failed.';
    throw new Error(
      `${message}\n\nLocal artifact: ${output}\nRetry without rebuilding: eoas build:upload ${JSON.stringify(
        output
      )}`
    );
  }
  return output;
}

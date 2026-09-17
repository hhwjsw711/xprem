import spawnAsync from '@expo/spawn-async';
import os from 'os';

export interface BuildMachine {
  hostname: string;
  os: string;
  arch: string;
  node: string;
  tools: Record<string, string>;
  ci?: string;
  ciRunUrl?: string;
}

// The computer running the build, recorded with it on the server.
export async function describeMachine(
  tools: Record<string, string>,
  env: NodeJS.ProcessEnv = process.env
): Promise<BuildMachine> {
  return {
    hostname: os.hostname(),
    os: await operatingSystem(),
    arch: os.arch(),
    node: process.versions.node,
    tools,
    ...continuousIntegration(env),
  };
}

async function operatingSystem(): Promise<string> {
  if (os.platform() === 'darwin') {
    const version = await spawnAsync('sw_vers', ['-productVersion']).then(
      ({ stdout }) => stdout.trim(),
      () => ''
    );
    if (version) {
      return `macOS ${version}`;
    }
  }
  return `${os.type()} ${os.release()}`;
}

export function continuousIntegration(env: NodeJS.ProcessEnv): { ci?: string; ciRunUrl?: string } {
  if (env.GITHUB_ACTIONS) {
    const { GITHUB_SERVER_URL: server, GITHUB_REPOSITORY: repository, GITHUB_RUN_ID: run } = env;
    return {
      ci: 'GitHub Actions',
      ciRunUrl:
        server && repository && run ? `${server}/${repository}/actions/runs/${run}` : undefined,
    };
  }
  if (env.GITLAB_CI) {
    return { ci: 'GitLab CI', ciRunUrl: env.CI_JOB_URL };
  }
  if (env.CIRCLECI) {
    return { ci: 'CircleCI', ciRunUrl: env.CIRCLE_BUILD_URL };
  }
  if (env.BITRISE_IO) {
    return { ci: 'Bitrise', ciRunUrl: env.BITRISE_BUILD_URL };
  }
  return env.CI ? { ci: 'CI' } : {};
}

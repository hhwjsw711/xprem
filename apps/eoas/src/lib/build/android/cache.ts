import spawnAsync from '@expo/spawn-async';
import fs from 'fs-extra';
import path from 'path';

import { startCacheRelay } from '../cache';
import { LogWriter } from '../log';
import { BuildInputs } from '../prepare';
import { copyTemplate } from '../workspace';

interface AndroidCache {
  args: string[];
  env: NodeJS.ProcessEnv;
}

export async function withAndroidCache<T>(
  build: Pick<BuildInputs, 'endpoint' | 'env' | 'options'>,
  working: string,
  temporary: string,
  log: LogWriter,
  compile: (cache: AndroidCache) => Promise<T>
): Promise<T> {
  if (build.options.remoteCache === false) {
    log.info('Remote cache disabled.');
    return await compile({ args: [], env: {} });
  }
  const script = path.join(temporary, 'cache.gradle');
  let relay;
  try {
    await copyTemplate('gradle/cache.gradle', script);
    relay = await startCacheRelay(build.endpoint, temporary, log);
  } catch {
    log.warn('Could not prepare remote cache; compiling without remote cache.');
    return await compile({ args: [], env: {} });
  }
  try {
    const project = await fs.realpath(working);
    const ccache = await findCcache(build.env);
    if (!ccache) {
      log.warn(
        'C/C++ remote cache unavailable: install ccache 4.4 or later with HTTP support. Gradle remote cache remains enabled.'
      );
    }
    log.info(`Remote cache enabled: Gradle${ccache ? ' and ccache' : ''}.`);
    return await compile({
      args: ['--build-cache', '--init-script', script],
      env: {
        EOAS_GRADLE_CACHE_URL: `${relay.url}/gradle/`,
        EOAS_CCACHE: ccache ?? '',
        EOAS_CACHE_PROJECT_DIR: project,
        ...(ccache
          ? {
              CCACHE_REMOTE_STORAGE: `${relay.url}/ccache|operation-timeout=60000`,
              CCACHE_BASEDIR: project,
              CCACHE_COMPILERCHECK: 'content',
              CCACHE_RESHARE: 'true',
            }
          : {}),
      },
    });
  } finally {
    await relay.close();
  }
}

async function findCcache(env: NodeJS.ProcessEnv): Promise<string | undefined> {
  for (const directory of (env.PATH ?? process.env.PATH ?? '').split(path.delimiter)) {
    const executable = path.resolve(directory, 'ccache');
    try {
      await fs.access(executable, fs.constants.X_OK);
      const { stdout } = await spawnAsync(executable, ['--version'], { env });
      const version = /ccache version (\d+)\.(\d+)/.exec(stdout);
      if (version && Number(version[1]) === 4 && Number(version[2]) >= 4 && /http/i.test(stdout)) {
        return executable;
      }
    } catch {
      /* Try the next PATH entry. */
    }
  }
  return undefined;
}

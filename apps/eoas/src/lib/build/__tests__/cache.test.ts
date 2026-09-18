import { execFile } from 'child_process';
import { createHash, randomUUID } from 'crypto';
import fs from 'fs-extra';
import { createServer } from 'http';
import fetch from 'node-fetch';
import os from 'os';
import path from 'path';
import { promisify } from 'util';
import { afterEach, expect, it, vi } from 'vitest';

import { withAndroidCache } from '../android/cache';
import { startCacheRelay } from '../cache';
import { BuildServerError, request } from '../server';

vi.mock('../server', async importOriginal => ({
  ...(await importOriginal<typeof import('../server')>()),
  request: vi.fn(),
}));
vi.mock('../../auth', () => ({
  retrieveCredentials: () => ({ token: 'test-token' }),
  getAuthHeaders: () => ({ Authorization: 'Bearer test-token' }),
}));
const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) {
    await cleanup();
  }
  vi.resetAllMocks();
});

it('publishes only after uploading bytes and verifies downloads before serving Gradle', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-cache-test-'));
  cleanups.push(() => fs.remove(directory));
  let bytes = Buffer.alloc(0);
  let uploads = 0;
  const bucket = createServer(async (req, res) => {
    expect(req.headers.authorization).toBeUndefined();
    if (req.method === 'PUT') {
      uploads++;
      const chunks = [];
      for await (const chunk of req) {
        chunks.push(chunk);
      }
      bytes = Buffer.concat(chunks);
      res.writeHead(200).end();
    } else {
      res.end(bytes);
    }
  });
  await new Promise<void>(resolve => bucket.listen(0, '127.0.0.1', resolve));
  cleanups.push(
    () =>
      new Promise<void>(resolve =>
        bucket.close(() => {
          resolve();
        })
      )
  );
  const bucketUrl = `http://127.0.0.1:${(bucket.address() as { port: number }).port}/object`;
  let object: Record<string, unknown>;
  vi.mocked(request).mockImplementation(async (url, options) => {
    if (url.endsWith('/uploads')) {
      if (object) {
        return { object, cached: true };
      }
      object = { id: 'upload-1', ...(options?.body as object) };
      return {
        object,
        upload: { url: bucketUrl, method: 'PUT', headers: { 'x-upload-test': 'yes' } },
      };
    }
    if (url.endsWith('/complete')) {
      expect(bytes.toString()).toBe('compiled bytes');
      return object;
    }
    return { object, url: bucketUrl };
  });
  const log = { info: vi.fn(), warn: vi.fn(), write: vi.fn() };
  const relay = await startCacheRelay('https://xprem.test/app/build/identifier', directory, log);
  cleanups.push(() => relay.close());
  const url = `${relay.url}/gradle/${'a'.repeat(32)}`;
  expect((await fetch(url, { method: 'PUT', body: 'compiled bytes' })).status).toBe(204);
  expect((await fetch(url, { method: 'PUT', body: 'compiled bytes' })).status).toBe(204);
  expect(uploads).toBe(1);
  expect(await (await fetch(url)).text()).toBe('compiled bytes');
  bytes = Buffer.from('corrupt bytes!');
  expect((await fetch(url)).status).toBe(404);
  expect(log.warn).toHaveBeenCalledTimes(1);
});

it('cancels pending remote requests when the build closes its relay', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-cache-test-'));
  cleanups.push(() => fs.remove(directory));
  let started!: () => void;
  const pending = new Promise<void>(resolve => {
    started = resolve;
  });
  let signal: AbortSignal | undefined;
  vi.mocked(request).mockImplementation(async (_url, options) => {
    signal = options!.signal;
    return await new Promise((_resolve, reject) => {
      signal!.addEventListener(
        'abort',
        () => {
          reject(new Error('Aborted'));
        },
        { once: true }
      );
      started();
    });
  });
  const relay = await startCacheRelay('https://xprem.test', directory, {
    info() {},
    warn() {},
    write() {},
  });
  cleanups.push(() => relay.close());
  const response = fetch(`${relay.url}/gradle/${'a'.repeat(32)}`).catch(() => undefined);
  await pending;
  await relay.close();
  await response;
  expect(signal?.aborted).toBe(true);
  expect(await fs.readdir(directory)).toEqual([]);
});

it('rejects unauthenticated local requests without contacting xprem', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-cache-test-'));
  cleanups.push(() => fs.remove(directory));
  const relay = await startCacheRelay('https://xprem.test/app/build/identifier', directory, {
    info() {},
    warn() {},
    write() {},
  });
  cleanups.push(() => relay.close());
  const url = new URL(relay.url);
  url.pathname = `/wrong-token/gradle/${'a'.repeat(32)}`;
  expect((await fetch(url)).status).toBe(404);
  expect(request).not.toHaveBeenCalled();
});

it('does not contact the cache or inject tools with --no-remote-cache', async () => {
  const log = { info: vi.fn(), warn: vi.fn(), write: vi.fn() };
  await withAndroidCache(
    { endpoint: 'https://xprem.test', options: { profile: 'test', remoteCache: false }, env: {} },
    '/unused',
    '/unused',
    log,
    async cache => {
      expect(cache).toEqual({ args: [], env: {} });
    }
  );
  expect(request).not.toHaveBeenCalled();
});

it('compiles without remote cache when its Gradle script cannot be installed', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-cache-test-'));
  cleanups.push(() => fs.remove(directory));
  const temporary = path.join(directory, 'not-a-directory');
  await fs.writeFile(temporary, 'occupied');
  const compile = vi.fn().mockResolvedValue('built');
  const log = { info: vi.fn(), warn: vi.fn(), write: vi.fn() };
  const result = await withAndroidCache(
    { endpoint: 'https://xprem.test', options: { profile: 'test' }, env: {} },
    directory,
    temporary,
    log,
    compile
  );
  expect(result).toBe('built');
  expect(compile).toHaveBeenCalledTimes(1);
  expect(compile).toHaveBeenCalledWith({ args: [], env: {} });
  expect(log.warn).toHaveBeenCalledTimes(1);
  expect(request).not.toHaveBeenCalled();
});

it('stops contacting an unavailable server and leaves builds free to compile', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-cache-test-'));
  cleanups.push(() => fs.remove(directory));
  vi.mocked(request).mockRejectedValue(new BuildServerError(503));
  const log = { info: vi.fn(), warn: vi.fn(), write: vi.fn() };
  const relay = await startCacheRelay('https://xprem.test', directory, log);
  cleanups.push(() => relay.close());
  const url = `${relay.url}/gradle/${'a'.repeat(32)}`;
  expect((await fetch(url)).status).toBe(404);
  expect((await fetch(url, { method: 'PUT', body: 'compiled bytes' })).status).toBe(204);
  expect((await fetch(url)).status).toBe(404);
  expect(request).toHaveBeenCalledTimes(1);
  expect(log.warn).toHaveBeenCalledTimes(1);
});

// Opt-in protocol test: independent workspaces and local caches, real clients.
it.runIf(process.env.TEST_GRADLE && process.env.TEST_CCACHE)(
  'reuses Gradle and ccache results across empty local caches',
  async () => {
    const root = await fs.mkdtemp(path.join(os.tmpdir(), 'eoas-cache-clients-'));
    cleanups.push(() => fs.remove(root));
    type CacheObject = { id: string; namespace: string; key: string; size: number; sha256: string };
    const objects = new Map<string, CacheObject>();
    const published = new Map<string, string>();
    const bytes = new Map<string, Buffer>();
    const bucket = createServer(async (req, res) => {
      const id = req.url!.slice(1);
      if (req.method === 'PUT') {
        const chunks: Buffer[] = [];
        for await (const chunk of req) {
          chunks.push(chunk);
        }
        bytes.set(id, Buffer.concat(chunks));
        res.end();
      } else if (bytes.has(id)) {
        res.end(bytes.get(id));
      } else {
        res.writeHead(404).end();
      }
    });
    await new Promise<void>(resolve => bucket.listen(0, '127.0.0.1', resolve));
    cleanups.push(
      () =>
        new Promise<void>(resolve =>
          bucket.close(() => {
            resolve();
          })
        )
    );
    const bucketUrl = `http://127.0.0.1:${(bucket.address() as { port: number }).port}`;
    vi.mocked(request).mockImplementation(async (url, options) => {
      if (url.endsWith('/uploads')) {
        const object = {
          id: randomUUID(),
          ...(options!.body as Omit<CacheObject, 'id'>),
        };
        objects.set(object.id, object);
        return { object, upload: { url: `${bucketUrl}/${object.id}`, method: 'PUT' } };
      }
      if (url.endsWith('/complete')) {
        const id = url.split('/').at(-2)!;
        const object = objects.get(id)!;
        expect(bytes.get(id)?.length).toBe(object.size);
        expect(createHash('sha256').update(bytes.get(id)!).digest('hex')).toBe(object.sha256);
        published.set(`${object.namespace}/${object.key}`, id);
        return object;
      }
      const id = published.get(url.split('/cache/')[1]);
      if (!id) {
        throw new BuildServerError(404);
      }
      return { object: objects.get(id), url: `${bucketUrl}/${id}` };
    });
    const exec = promisify(execFile);
    const log = { info: vi.fn(), warn: vi.fn(), write: vi.fn() };
    for (const machine of ['one', 'two']) {
      const working = path.join(root, machine);
      await fs.ensureDir(path.join(working, 'src/main/java'));
      await fs.writeFile(
        path.join(working, 'settings.gradle'),
        "rootProject.name = 'cache-test'\n"
      );
      await fs.writeFile(path.join(working, 'build.gradle'), "plugins { id 'java' }\n");
      await fs.writeFile(
        path.join(working, 'src/main/java/Answer.java'),
        'class Answer { int value() { return 42; } }\n'
      );
      await fs.writeFile(path.join(working, 'answer.c'), 'int answer(void) { return 42; }\n');
      const env = {
        ...process.env,
        PATH: `${path.dirname(process.env.TEST_CCACHE!)}:${process.env.PATH}`,
        CCACHE_DIR: path.join(working, 'ccache'),
        GRADLE_USER_HOME: path.join(working, 'gradle-home'),
      };
      await withAndroidCache(
        { endpoint: 'https://xprem.test/app/build/id', options: { profile: 'test' }, env },
        working,
        working,
        log,
        async cache => {
          const childEnv = { ...env, ...cache.env };
          const gradle = await exec(
            process.env.TEST_GRADLE!,
            ['compileJava', '--no-daemon', '--console=plain', ...cache.args],
            { cwd: working, env: childEnv, timeout: 120000 }
          );
          if (machine === 'two') {
            expect(gradle.stdout).toContain(':compileJava FROM-CACHE');
          }
          await exec(
            process.env.TEST_CCACHE!,
            [
              'clang',
              '-g',
              '-c',
              'answer.c',
              '-o',
              'answer.o',
              `-ffile-prefix-map=${cache.env.EOAS_CACHE_PROJECT_DIR}=.`,
            ],
            { cwd: working, env: childEnv, timeout: 120000 }
          );
          const ccache = await exec(process.env.TEST_CCACHE!, ['--show-stats', '--verbose'], {
            cwd: working,
            env: childEnv,
          });
          if (machine === 'two') {
            expect(ccache.stdout).toMatch(/Hits:\s+1\s/);
          }
        }
      );
    }
    expect([...published.keys()].some(key => key.startsWith('gradle/'))).toBe(true);
    expect([...published.keys()].some(key => key.startsWith('ccache/'))).toBe(true);
    expect(log.warn).not.toHaveBeenCalled();
  },
  180000
);

import { createHash, randomBytes, randomUUID } from 'crypto';
import fs from 'fs-extra';
import { IncomingMessage, ServerResponse, createServer } from 'http';
import fetch from 'node-fetch';
import path from 'path';
import { Readable, Transform } from 'stream';
import { pipeline } from 'stream/promises';

import { LogWriter } from './log';
import { BuildServerError, request } from './server';
import { assertSafeUploadUrl } from '../assets';
import { getAuthHeaders, retrieveCredentials } from '../auth';

const MAX_OBJECT_BYTES = 512 * 1024 * 1024;
const TRANSFER_TIMEOUT = 120000;
const MAX_TRANSFERS = 8;

interface CacheObject {
  id: string;
  namespace: string;
  key: string;
  size: number;
  sha256: string;
}

export interface CacheRelay {
  url: string;
  close(): Promise<void>;
}

// Tools speak their HTTP cache protocol on loopback. Only this relay holds the
// xprem credentials and translates uploads into reserve/upload/complete calls.
export async function startCacheRelay(
  endpoint: string,
  directory: string,
  log: LogWriter
): Promise<CacheRelay> {
  const token = randomBytes(24).toString('hex');
  const active = new Set<AbortController>();
  const tasks = new Set<Promise<void>>();
  const waiting: [IncomingMessage, ServerResponse][] = [];
  const stats = { hits: 0, misses: 0, uploads: 0, downloaded: 0, uploaded: 0 };
  let warned = false;
  let unavailable = false;
  let closing = false;
  const warn = (): void => {
    if (!warned && !closing) {
      warned = true;
      log.warn('Some remote cache operations failed; affected tasks will compile normally.');
    }
  };
  const summary = (): string =>
    `Remote cache: ${stats.hits} hits, ${stats.misses} misses, ${stats.uploads} uploads; ` +
    `${(stats.downloaded / 1048576).toFixed(1)} MB received, ${(stats.uploaded / 1048576).toFixed(
      1
    )} MB sent.`;

  const handle = async (req: IncomingMessage, res: ServerResponse): Promise<void> => {
    const prefix = `/${token}/`;
    const route = req.url?.startsWith(prefix) ? req.url.slice(prefix.length) : '';
    // ccache's default layout splits its key after two characters.
    const match = /^(gradle|ccache)\/([a-zA-Z0-9_-]+)(?:\/([a-zA-Z0-9_-]+))?$/.exec(route);
    if (!match || (match[1] === 'gradle' && (match[3] || !/^[a-f0-9]{32}$/.test(match[2])))) {
      res.writeHead(404).end();
      return;
    }
    const namespace = match[1];
    const key = match[2] + (match[3] ?? '');
    if (key.length > 128) {
      res.writeHead(400).end();
      return;
    }
    if (req.method === 'DELETE') {
      // Shared eviction is server-owned. A local ccache cleanup cannot evict
      // another machine's entries; manifests are replaced through PUT.
      res.writeHead(204).end();
      return;
    }
    if (!['GET', 'PUT', 'HEAD'].includes(req.method ?? '')) {
      res.writeHead(405).end();
      return;
    }
    if (unavailable) {
      res.writeHead(req.method === 'PUT' ? 204 : 404).end();
      return;
    }
    const controller = new AbortController();
    active.add(controller);
    const timeout = setTimeout(() => {
      controller.abort();
    }, TRANSFER_TIMEOUT);
    const disconnect = (): void => {
      if (!res.writableFinished) {
        controller.abort();
      }
    };
    res.on('close', disconnect);
    const file = path.join(directory, `cache-${randomUUID()}`);
    try {
      if (req.method === 'PUT') {
        const content = await spool(req, file, MAX_OBJECT_BYTES, controller.signal);
        if (content.size === 0) {
          res.writeHead(413).end();
          return;
        }
        const registration = await request<{
          object: CacheObject;
          cached?: boolean;
          upload?: { url: string; method: string; headers?: Record<string, string> };
        }>(`${endpoint}/cache/uploads`, {
          method: 'POST',
          retry: false,
          body: { namespace, key, ...content },
          timeout: 15000,
          signal: controller.signal,
        });
        const upload = registration.upload;
        const object = registration.object;
        if (!object?.id || object.size !== content.size || object.sha256 !== content.sha256) {
          throw new Error('Invalid cache registration.');
        }
        if (registration.cached) {
          res.writeHead(204).end();
          return;
        }
        const uploadUrl =
          upload?.url ?? `${endpoint}/cache/uploads/${encodeURIComponent(object.id)}`;
        assertSafeUploadUrl(uploadUrl);
        if (upload && upload.method !== 'PUT') {
          throw new Error('Invalid upload method.');
        }
        const body = fs.createReadStream(file);
        try {
          const response = await fetch(uploadUrl, {
            method: 'PUT',
            body,
            redirect: 'error',
            signal: controller.signal,
            headers: {
              ...(upload ? upload.headers : getAuthHeaders(retrieveCredentials())),
              'Content-Length': String(content.size),
            },
          });
          (response.body as Readable).destroy();
          if (!response.ok) {
            throw new Error('Cache upload failed.');
          }
        } finally {
          body.destroy();
        }
        const completed = await request<CacheObject>(
          `${endpoint}/cache/uploads/${encodeURIComponent(object.id)}/complete`,
          {
            method: 'POST',
            retry: false,
            timeout: TRANSFER_TIMEOUT,
            signal: controller.signal,
          }
        );
        if (completed.id !== object.id || completed.sha256 !== content.sha256) {
          throw new Error('Cache publication failed.');
        }
        stats.uploads++;
        stats.uploaded += content.size;
        res.writeHead(204).end();
      } else {
        const found = await request<{ object: CacheObject; url: string }>(
          `${endpoint}/cache/${namespace}/${encodeURIComponent(key)}`,
          { retry: false, timeout: 15000, signal: controller.signal }
        );
        const object = found.object;
        if (
          !object?.id ||
          object.namespace !== namespace ||
          object.key !== key ||
          !Number.isSafeInteger(object.size) ||
          object.size < 1 ||
          object.size > MAX_OBJECT_BYTES ||
          !/^[a-f0-9]{64}$/.test(object.sha256)
        ) {
          throw new Error('Invalid cache entry.');
        }
        const url =
          found.url || `${endpoint}/cache/uploads/${encodeURIComponent(object.id)}/download`;
        assertSafeUploadUrl(url);
        const response = await fetch(url, {
          headers: found.url ? {} : getAuthHeaders(retrieveCredentials()),
          redirect: 'error',
          signal: controller.signal,
        });
        if (!response.ok) {
          (response.body as Readable).destroy();
          throw new Error('Cache download failed.');
        }
        const content = await spool(
          response.body as Readable,
          file,
          object.size,
          controller.signal
        );
        if (content.size !== object.size || content.sha256 !== object.sha256) {
          throw new Error('Cache integrity check failed.');
        }
        res.writeHead(200, {
          'Content-Length': String(content.size),
          'Content-Type': 'application/octet-stream',
        });
        if (req.method === 'HEAD') {
          res.end();
        } else {
          await pipeline(fs.createReadStream(file), res, { signal: controller.signal });
        }
        stats.hits++;
        stats.downloaded += content.size;
      }
    } catch (error) {
      const miss =
        error instanceof BuildServerError && error.status === 404 && req.method !== 'PUT';
      if (!miss) {
        // Avoid paying a network timeout for every remaining compiler invocation.
        unavailable = true;
        warn();
      }
      if (req.method !== 'PUT') {
        stats.misses++;
      }
      if (!res.headersSent && !res.destroyed) {
        // Cache failures never turn a successful compilation into a failed build.
        res.writeHead(req.method === 'PUT' ? 204 : 404).end();
      } else if (!res.writableFinished) {
        res.destroy();
      }
    } finally {
      clearTimeout(timeout);
      res.removeListener('close', disconnect);
      active.delete(controller);
      await fs.remove(file);
    }
  };
  const dispatch = (req: IncomingMessage, res: ServerResponse): void => {
    if (closing || waiting.length >= 64) {
      res.writeHead(503).end();
      return;
    }
    if (tasks.size >= MAX_TRANSFERS) {
      waiting.push([req, res]);
      return;
    }
    const task = handle(req, res).catch(() => {
      warn();
      res.destroy();
    });
    tasks.add(task);
    void task.finally(() => {
      tasks.delete(task);
      while (waiting.length) {
        const [nextRequest, nextResponse] = waiting.shift()!;
        if (!nextResponse.destroyed) {
          dispatch(nextRequest, nextResponse);
          break;
        }
      }
    });
  };
  const server = createServer(dispatch);
  server.requestTimeout = TRANSFER_TIMEOUT;
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      server.removeListener('error', reject);
      resolve();
    });
  });
  const progress = setInterval(() => {
    if (active.size) {
      log.write(summary());
    }
  }, 10000);
  progress.unref();
  let closed: Promise<void> | undefined;
  return {
    url: `http://127.0.0.1:${(server.address() as { port: number }).port}/${token}`,
    close() {
      return (closed ??= (async () => {
        closing = true;
        clearInterval(progress);
        for (const controller of active) {
          controller.abort();
        }
        await new Promise<void>(resolve => {
          server.close(() => {
            resolve();
          });
          server.closeAllConnections();
        });
        await Promise.allSettled(tasks);
        log.info(summary());
      })());
    },
  };
}

async function spool(
  input: Readable,
  file: string,
  limit: number,
  signal: AbortSignal
): Promise<{ size: number; sha256: string }> {
  const hash = createHash('sha256');
  let size = 0;
  const measure = new Transform({
    transform(chunk: Buffer, _encoding, callback) {
      size += chunk.length;
      if (size > limit) {
        callback(new Error('Cache entry exceeds its size limit.'));
        return;
      }
      hash.update(chunk);
      callback(null, chunk);
    },
  });
  await pipeline(input, measure, fs.createWriteStream(file, { flags: 'wx', mode: 0o600 }), {
    signal,
  });
  return { size, sha256: hash.digest('hex') };
}

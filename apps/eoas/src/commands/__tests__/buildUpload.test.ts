import { ExpoConfig } from '@expo/config';
import path from 'path';
import { beforeEach, expect, it, vi } from 'vitest';

import { uploadBuildArtifact } from '../../lib/build/artifacts';
import { BuildLog } from '../../lib/build/log';
import { getPrivateExpoConfigAsync } from '../../lib/expoConfig';
import BuildUpload from '../build/upload';

vi.mock('../../lib/build/artifacts', () => ({ uploadBuildArtifact: vi.fn() }));
vi.mock('../../lib/build/log', () => ({
  withBuildLog: async (
    _project: string,
    _profile: string,
    _title: string,
    upload: (log: BuildLog) => Promise<string>
  ) => await upload({} as BuildLog),
}));
vi.mock('../../lib/expoConfig', async importOriginal => ({
  ...(await importOriginal<typeof import('../../lib/expoConfig')>()),
  getPrivateExpoConfigAsync: vi.fn(),
}));
vi.mock('../../lib/log', () => ({ default: { succeed: vi.fn(), error: vi.fn() } }));

const eoasRoot = path.resolve(__dirname, '../../..');
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(getPrivateExpoConfigAsync).mockResolvedValue({
    updates: { url: 'http://configured.example.com/ota/manifest' },
  } as ExpoConfig);
  vi.mocked(uploadBuildArtifact).mockResolvedValue('build-id');
});

it('selects the configured server with resolveServerUrl for upload retries', async () => {
  await BuildUpload.run(['test.apk'], eoasRoot);
  expect(getPrivateExpoConfigAsync).toHaveBeenCalledWith(process.cwd());
  expect(uploadBuildArtifact).toHaveBeenCalledWith(
    path.resolve('test.apk'),
    'http://configured.example.com/ota',
    expect.anything()
  );
});

it('uses explicit --serverUrl without requiring the project config to evaluate', async () => {
  vi.mocked(getPrivateExpoConfigAsync).mockRejectedValue(new Error('Missing build environment'));
  await BuildUpload.run(
    ['test.apk', '--serverUrl', 'http://override.example.com/builds/manifest/'],
    eoasRoot
  );
  expect(getPrivateExpoConfigAsync).not.toHaveBeenCalled();
  expect(uploadBuildArtifact).toHaveBeenCalledWith(
    path.resolve('test.apk'),
    'http://override.example.com/builds',
    expect.anything()
  );
});

it('refuses to upload without a configured server or explicit override', async () => {
  vi.mocked(getPrivateExpoConfigAsync).mockResolvedValue({} as ExpoConfig);
  await expect(BuildUpload.run(['test.apk'], eoasRoot)).rejects.toThrow();
  expect(uploadBuildArtifact).not.toHaveBeenCalled();
});

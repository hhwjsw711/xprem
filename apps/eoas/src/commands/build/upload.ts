import { ExpoConfig } from '@expo/config';
import { Args, Command, Flags } from '@oclif/core';
import path from 'path';

import { uploadBuildArtifact } from '../../lib/build/artifacts';
import { withBuildLog } from '../../lib/build/log';
import { getPrivateExpoConfigAsync, resolveServerUrl } from '../../lib/expoConfig';
import Log from '../../lib/log';

export default class BuildUpload extends Command {
  static override description =
    'Retry uploading a local APK/AAB using its adjacent .build.json metadata, without rebuilding or reserving another build number.';
  static override args = {
    artifact: Args.string({ required: true, description: 'Local APK/AAB path' }),
  };
  static override flags = {
    serverUrl: Flags.string({ description: 'Override updates.url for the build server' }),
  };
  async run(): Promise<void> {
    const { args, flags } = await this.parse(BuildUpload);
    try {
      const config = flags.serverUrl
        ? ({} as ExpoConfig)
        : await getPrivateExpoConfigAsync(process.cwd());
      const server = await resolveServerUrl(config, flags.serverUrl);
      const id = await withBuildLog(
        process.cwd(),
        'upload',
        'Artifact upload',
        async log => await uploadBuildArtifact(path.resolve(args.artifact), server, log)
      );
      Log.succeed(`Build ${id} is available in the dashboard.`);
    } catch (error) {
      Log.error(error instanceof Error ? error.message : 'Artifact upload failed.');
      this.exit(1);
    }
  }
}

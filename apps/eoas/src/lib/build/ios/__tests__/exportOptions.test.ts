import { expect, it } from 'vitest';

import { exportOptionsPlist } from '../index';

const build = {
  xcodeMajor: 16,
  ios: { bundleIdentifier: 'com.example.app', distribution: 'ad-hoc' as const },
  credentials: {
    teamId: 'ABCDE12345',
    certificateP12: '',
    certificatePassword: '',
    provisioningProfile: '',
  },
};

it('repeats the iCloud environment of the app in the export options', () => {
  expect(exportOptionsPlist(build, 'profile-uuid')).not.toContain('iCloudContainerEnvironment');
  expect(exportOptionsPlist(build, 'profile-uuid', 'Production')).toContain(
    '<key>iCloudContainerEnvironment</key>\n  <string>Production</string>'
  );
});

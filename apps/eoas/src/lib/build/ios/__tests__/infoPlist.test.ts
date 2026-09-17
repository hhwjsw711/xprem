import { expect, it } from 'vitest';

import { targetInfoPlist } from '../index';

it('finds the Info.plist a target names in its build settings', () => {
  expect(targetInfoPlist('/work', 'MyApp/Info.plist')).toBe('/work/ios/MyApp/Info.plist');
  expect(targetInfoPlist('/work', '"$(SRCROOT)/My App/Staging-Info.plist"')).toBe(
    '/work/ios/My App/Staging-Info.plist'
  );
  expect(targetInfoPlist('/work', '$(TARGET_NAME)/Info.plist')).toBeUndefined();
  expect(targetInfoPlist('/work')).toBeUndefined();
});

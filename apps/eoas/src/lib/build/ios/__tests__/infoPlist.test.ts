import { expect, it } from 'vitest';

import { targetFile } from '../index';

it('finds the Info.plist a target names in its build settings', () => {
  expect(targetFile('/work', 'MyApp/Info.plist')).toBe('/work/ios/MyApp/Info.plist');
  expect(targetFile('/work', '"$(SRCROOT)/My App/Staging-Info.plist"')).toBe(
    '/work/ios/My App/Staging-Info.plist'
  );
  expect(targetFile('/work', '$(TARGET_NAME)/Info.plist')).toBeUndefined();
  expect(targetFile('/work')).toBeUndefined();
});

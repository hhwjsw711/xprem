import { expect, it } from 'vitest';

import { matchXcode } from '../xcode';

it('matches the newest installed Xcode that starts with the requested version', () => {
  const installed = [
    { version: '27.0', developerDir: '/Applications/Xcode.app/Contents/Developer' },
    { version: '26.2', developerDir: '/Applications/Xcode-26.2.app/Contents/Developer' },
    { version: '26.0.1', developerDir: '/Applications/Xcode-26.0.1.app/Contents/Developer' },
  ];
  expect(matchXcode(installed, '26')?.version).toBe('26.2');
  expect(matchXcode(installed, '26.0\n')?.version).toBe('26.0.1');
  expect(matchXcode(installed, '27.0')?.version).toBe('27.0');
  expect(matchXcode(installed, '2')).toBeUndefined();
  expect(matchXcode(installed, '25')).toBeUndefined();
});

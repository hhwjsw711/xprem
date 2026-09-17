import { createHash } from 'crypto';
import { expect, it } from 'vitest';

import { assertProfileHoldsIdentity, parseCertificates, parseIdentities } from '../signing';

const certificate = Buffer.from('a distribution certificate');
const sha1 = createHash('sha1').update(certificate).digest('hex').toUpperCase();

it('reads the identities macOS lists for a keychain', () => {
  const output = `  1) ${sha1} "Apple Distribution: Example (ABCDE12345)"\n     1 valid identities found\n`;
  expect(parseIdentities(output)).toEqual([sha1]);
  expect(parseIdentities('     0 valid identities found\n')).toEqual([]);
});

it('reads the certificates of a provisioning profile, across wrapped lines', () => {
  const encoded = certificate.toString('base64');
  const xml = `<array>\n\t<data>\n\t${encoded.slice(0, 10)}\n\t${encoded.slice(
    10
  )}\n\t</data>\n</array>`;
  expect(parseCertificates(xml)).toEqual([sha1]);
});

it('refuses a profile that does not allow the signing certificate', () => {
  expect(() => {
    assertProfileHoldsIdentity('xprem app ad-hoc', [sha1], [sha1]);
  }).not.toThrow();
  expect(() => {
    assertProfileHoldsIdentity('xprem app ad-hoc', [sha1], ['0'.repeat(40)]);
  }).toThrow('does not allow the signing certificate');
});

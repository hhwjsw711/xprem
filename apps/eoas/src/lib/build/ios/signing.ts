import spawnAsync from '@expo/spawn-async';
import { spawnSync } from 'child_process';
import { createHash, randomBytes, randomUUID } from 'crypto';
import fs from 'fs-extra';
import os from 'os';
import path from 'path';

export interface IosCredentials {
  certificateP12: string;
  certificatePassword: string;
  provisioningProfile: string;
  teamId: string;
}

export interface InstalledSigning {
  keychain: string;
  profileName: string;
  profileUuid: string;
  teamId: string;
  // Synchronous so it also runs from the process exit hook of an interrupted build.
  remove(): void;
}

const PROFILE_DIRECTORIES = [
  'Library/MobileDevice/Provisioning Profiles',
  'Library/Developer/Xcode/UserData/Provisioning Profiles',
];

// Imports the certificate into a temporary keychain and installs the profile where Xcode looks for it.
export async function installSigning(
  credentials: IosCredentials,
  temporary: string
): Promise<InstalledSigning> {
  const certificate = path.join(temporary, 'certificate.p12');
  const profile = path.join(temporary, 'profile.mobileprovision');
  await fs.writeFile(certificate, Buffer.from(credentials.certificateP12, 'base64'), {
    mode: 0o600,
  });
  await fs.writeFile(profile, Buffer.from(credentials.provisioningProfile, 'base64'));
  const decoded = path.join(temporary, 'profile.plist');
  await run(
    'security',
    ['cms', '-D', '-i', profile, '-o', decoded],
    'read the provisioning profile'
  );
  const profileUuid = await plistValue(decoded, 'UUID');
  const profileName = await plistValue(decoded, 'Name');

  const keychain = path.join(temporary, 'eoas-build.keychain-db');
  const keychainPassword = randomBytes(24).toString('hex');
  const installedProfiles: string[] = [];
  let removed = false;
  const remove = (): void => {
    if (removed) {
      return;
    }
    removed = true;
    process.removeListener('exit', remove);
    // Also takes the keychain out of the search list, leaving the entries of other builds alone.
    spawnSync('security', ['delete-keychain', keychain]);
    for (const installed of installedProfiles) {
      // eslint-disable-next-line node/no-sync
      fs.removeSync(installed);
    }
  };
  process.once('exit', remove);
  try {
    await run(
      'security',
      ['create-keychain', '-p', keychainPassword, keychain],
      'create a keychain'
    );
    await run('security', ['set-keychain-settings', keychain], 'configure the keychain');
    await run(
      'security',
      ['unlock-keychain', '-p', keychainPassword, keychain],
      'unlock the keychain'
    );
    await run(
      'security',
      [
        'import',
        certificate,
        '-f',
        'pkcs12',
        '-k',
        keychain,
        '-P',
        credentials.certificatePassword,
        '-T',
        '/usr/bin/codesign',
        '-T',
        '/usr/bin/security',
      ],
      'import the signing certificate'
    );
    await run(
      'security',
      [
        'set-key-partition-list',
        '-S',
        'apple-tool:,apple:,codesign:',
        '-s',
        '-k',
        keychainPassword,
        keychain,
      ],
      'authorize codesign to use the certificate'
    );
    assertProfileHoldsIdentity(
      profileName,
      await signingIdentities(keychain),
      await profileCertificates(decoded)
    );
    const searchList = (await keychainSearchList()).filter(entry => entry !== keychain);
    await run(
      'security',
      ['list-keychains', '-d', 'user', '-s', keychain, ...searchList],
      'register the keychain'
    );
    // Xcode reads the profile inside the file, so each build installs and removes a file of its own.
    const fileName = `${randomUUID()}.mobileprovision`;
    for (const directory of PROFILE_DIRECTORIES) {
      const installed = path.join(os.homedir(), directory, fileName);
      await fs.ensureDir(path.dirname(installed));
      installedProfiles.push(installed);
      await fs.copyFile(profile, installed);
    }
  } catch (error) {
    remove();
    throw error;
  }
  return { keychain, profileName, profileUuid, teamId: credentials.teamId, remove };
}

// The SHA-1 of the certificates macOS accepts for signing in a keychain.
async function signingIdentities(keychain: string): Promise<string[]> {
  const list = async (...flags: string[]): Promise<string[]> => {
    const { stdout } = await spawnAsync('security', [
      'find-identity',
      ...flags,
      '-p',
      'codesigning',
      keychain,
    ]);
    return parseIdentities(stdout);
  };
  const valid = await list('-v');
  if (valid.length === 0) {
    throw new Error(
      (await list()).length === 0
        ? 'The signing certificate holds no private key, so it cannot sign.'
        : 'macOS does not accept the signing certificate: it is expired or revoked, or the Apple Worldwide Developer Relations intermediate certificate is missing on this machine.'
    );
  }
  return valid;
}

export function parseIdentities(output: string): string[] {
  return [...output.matchAll(/^\s*\d+\) ([0-9A-F]{40}) /gm)].map(([, sha1]) => sha1);
}

// The SHA-1 of the certificates a decoded provisioning profile allows.
async function profileCertificates(decoded: string): Promise<string[]> {
  const { stdout } = await spawnAsync('plutil', [
    '-extract',
    'DeveloperCertificates',
    'xml1',
    '-o',
    '-',
    decoded,
  ]);
  return parseCertificates(stdout);
}

export function parseCertificates(xml: string): string[] {
  return [...xml.matchAll(/<data>([\s\S]*?)<\/data>/g)].map(([, encoded]) =>
    createHash('sha1').update(Buffer.from(encoded, 'base64')).digest('hex').toUpperCase()
  );
}

export function assertProfileHoldsIdentity(
  profileName: string,
  identities: string[],
  certificates: string[]
): void {
  if (!identities.some(identity => certificates.includes(identity))) {
    throw new Error(
      `The provisioning profile "${profileName}" does not allow the signing certificate (${identities.join(
        ', '
      )}). Check the iOS signing settings of this app on the server.`
    );
  }
}

async function keychainSearchList(): Promise<string[]> {
  const { stdout } = await spawnAsync('security', ['list-keychains', '-d', 'user']);
  return stdout
    .split('\n')
    .map(line => line.trim().replace(/^"|"$/g, ''))
    .filter(Boolean);
}

async function plistValue(file: string, key: string): Promise<string> {
  const { stdout } = await spawnAsync('/usr/libexec/PlistBuddy', ['-c', `Print :${key}`, file]);
  const value = stdout.trim();
  if (!value) {
    throw new Error(`The provisioning profile has no ${key}.`);
  }
  return value;
}

// Arguments may hold passwords, so a failure reports the step and the tool's stderr only.
async function run(command: string, args: string[], step: string): Promise<void> {
  try {
    await spawnAsync(command, args);
  } catch (error) {
    const stderr = (error as { stderr?: string }).stderr?.trim();
    throw new Error(`Could not ${step}.${stderr ? ` ${stderr}` : ''}`);
  }
}

import spawnAsync from '@expo/spawn-async';
import { spawnSync } from 'child_process';
import { randomBytes, randomUUID } from 'crypto';
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

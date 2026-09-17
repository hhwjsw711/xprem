const fs = require('node:fs/promises');
const { createRequire } = require('node:module');
const path = require('node:path');

async function main() {
  const [project, fingerprintModule, output, platform] = process.argv.slice(2);
  const fingerprint = await require(fingerprintModule).createFingerprintAsync(project, {
    platforms: [platform],
    silent: true,
    ignorePaths: ['build-artifacts/**/*', '.eoas-export/**/*', 'android/local.properties'],
  });
  const projectRequire = createRequire(path.join(project, 'package.json'));
  const expoSdk = projectRequire('expo/package.json').version;
  await fs.writeFile(output, JSON.stringify({ fingerprint: fingerprint.hash, expoSdk }), {
    mode: 0o600,
  });
}

main().catch(error => {
  process.stderr.write(
    'Could not compute the Expo fingerprint. Check the project configuration and installed dependencies.\n' +
      `${error instanceof Error ? error.stack || error.message : String(error)}\n`
  );
  process.exitCode = 1;
});

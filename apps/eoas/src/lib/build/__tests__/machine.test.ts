import { expect, it } from 'vitest';

import { continuousIntegration, describeMachine } from '../machine';

it('describes the machine running the build', async () => {
  const machine = await describeMachine({ xcode: '16.4' }, {});
  expect(machine).toMatchObject({
    arch: process.arch,
    node: process.versions.node,
    tools: { xcode: '16.4' },
  });
  expect(machine.hostname).not.toBe('');
  expect(machine.os).not.toBe('');
  expect(machine.ci).toBeUndefined();
});

it('links the build to the run of the CI that started it', () => {
  expect(
    continuousIntegration({
      GITHUB_ACTIONS: 'true',
      GITHUB_SERVER_URL: 'https://github.com',
      GITHUB_REPOSITORY: 'acme/app',
      GITHUB_RUN_ID: '42',
    })
  ).toEqual({ ci: 'GitHub Actions', ciRunUrl: 'https://github.com/acme/app/actions/runs/42' });
  expect(
    continuousIntegration({ GITLAB_CI: 'true', CI_JOB_URL: 'https://gitlab.com/j/1' })
  ).toEqual({ ci: 'GitLab CI', ciRunUrl: 'https://gitlab.com/j/1' });
  expect(continuousIntegration({ CI: 'true' })).toEqual({ ci: 'CI' });
});

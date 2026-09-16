# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 3.x (latest release) | :white_check_mark: |
| < 3.0   | :x:                |

If a vulnerability affects older versions, the fix will land in the
latest release. Upgrading is the supported remediation path.

## Reporting a Vulnerability

Please do not open a public issue for security vulnerabilities.

Report privately via GitHub's private vulnerability reporting
("Report a vulnerability" in the Security tab of this repository).

You can expect:
- An acknowledgment within 48 hours
- An assessment and severity classification within 7 days
- A coordinated fix and disclosure if the report is accepted, with
  credit in the release notes unless you prefer otherwise

Given the nature of this project (an OTA update server delivering
code to end-user devices), reports affecting update integrity,
authentication, or code signing are treated with the highest priority.

## Verifying releases

Every Docker image and Helm chart is signed with [cosign](https://github.com/sigstore/cosign)
by the release workflow of this repository. Signatures are keyless: they are tied
to the GitHub Actions identity of the workflow and recorded in the public Sigstore
transparency log.

```
cosign verify ghcr.io/mercuretechnologies/xprem:vX.Y.Z \
  --certificate-identity-regexp '^https://github.com/mercuretechnologies/xprem/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The same command verifies the Helm chart with `ghcr.io/mercuretechnologies/charts/xprem:X.Y.Z`
as the reference.

# Security Policy

## Supported version

Security fixes are applied to the latest code on the protected `main` branch. There are no tagged release branches yet.

## Reporting a vulnerability

Please do not open a public issue or pull request for an exploitable vulnerability.

Use GitHub's private vulnerability reporting / Security Advisory workflow for this repository when it is enabled. Include:
- affected commit or component;
- concise impact and realistic attack prerequisites;
- minimal reproduction steps or proof of concept;
- suggested mitigation, if known.

Do not include real user data, credentials, tokens or private documents in the report. The maintainer will coordinate triage and a fix before public disclosure.

If private vulnerability reporting is unavailable in the repository UI, contact the repository maintainer privately through GitHub before sharing technical details publicly.

## Security expectations

- Treat tenant isolation, authorization, session handling, payment state, evidence access and audit integrity as security-sensitive.
- Never commit credentials or production personal data.
- Keep dependency, code scanning and secret-scanning findings actionable.
- Do not bypass protected-branch checks or fabricate review/verification evidence.

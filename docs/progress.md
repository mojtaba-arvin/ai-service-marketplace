# Development Progress

> Keep this file concise (maximum 60 lines). Update it at the end of each work session.

## Current phase
Foundation / Session 0A — repository and agent harness.

## Completed
- Confirmed repository is public and write access is available.
- Created working branch `foundation/session-0a` and draft PR #1.
- Added business and technical vision documents derived from the supplied product plan.
- Recorded ADRs for decision records, modular-monolith boundaries, and the initial stack.
- Added agent guidance, Copilot/Claude entry points, role profiles, issue forms, PR template, label definitions, and inactive CODEOWNERS placeholder.
- Added Go API, Next.js web, React admin, Flutter clients, Python AI worker, contracts, and infrastructure placeholders.
- Added feature-list JSON schema and invariant checker.
- Added Makefile verification entry points, JSON/shell checks, Docker Compose services, and full-mode health smoke checks.
- Added CI, CodeQL/dependency review, and conservative implementation-agent guard.
- Created follow-up issues: #2 repository protections/verification, #3 Session 0B backlog, #4 Session 0C walking skeleton.

## Verification
- GitHub branch and file writes confirmed.
- Local execution has not been performed in this session.
- No GitHub Actions workflow run is visible for the PR head yet; CI must be observed before treating the foundation as verified.

## Known limitations / blockers
- `feature_list.json` is intentionally empty; Session 0B must populate it with atomic, dependency-aware features.
- Agent guard currently rejects all implementation-agent edits to `feature_list.json`; a QA-only status update path remains to be designed.
- GitHub rulesets, required checks, Actions permissions, Dependabot, secret scanning, and CodeQL settings must be confirmed in repository settings.
- CODEOWNERS is inactive until real maintainer/team handles are configured.
- License is intentionally undecided; choose before the first public release.
- Initial market/jurisdiction remains a product/legal decision; payment, KYC, tax, and labor adapters must stay disabled until validated.

## Next
1. Observe and fix PR #1 CI findings; do not merge without passing checks.
2. Session 0B: decompose M1–M14 into small features, dependencies, acceptance criteria, and GitHub issues.
3. Session 0C: implement and independently verify the end-to-end walking skeleton.

# TRUST-002 Private Evidence — maintainer acceptance Matrix

**Scope:** Issue #217 and the private-evidence implementation proposed in PR #219.  
**Purpose:** Give an maintainer verification operator a reproducible, evidence-oriented test plan. This document is a test plan, not a claim that any scenario has passed.

## Preconditions

- Deploy the exact reviewed commit to an isolated test environment; record repository SHA, image digests, deployment time and configuration version.
- Use disposable test tenants A and B. Create distinct reporter identities, an active operator with the `incident:review` grant in tenant A, an operator without that grant, and a support-only identity.
- Configure a private evidence volume, a dedicated encryption key ring with an active key ID, an explicit allowed media-type list, bounded upload size/count and a reachable ClamAV endpoint. Keep secrets outside source control and test logs.
- Create one incident report per reporter. Record opaque report/evidence IDs only; never attach real personal or production evidence.
- Capture API responses, database audit rows, scanner test results and filesystem metadata without recording plaintext evidence, credentials, key material or storage paths in application logs.

## Acceptance scenarios

| ID | Scenario | Expected result / evidence |
|---|---|---|
| EVD-01 | Reporter uploads an allowed, clean file to their own report | Upload succeeds; response contains only opaque evidence ID, sniffed media type and byte count |
| EVD-02 | Reporter lists and downloads their own evidence | Metadata is listed; bytes round-trip exactly; response is private/no-store/nosniff |
| EVD-03 | Client supplies a misleading filename or MIME header | Filename/header do not control storage name or accepted media type; content is sniffed |
| EVD-04 | Unsupported, malformed, empty or oversized content | Rejected; no retrievable object or orphaned metadata remains |
| EVD-05 | Upload exceeds per-report item limit, including legacy references | Rejected without partial state |
| EVD-06 | ClamAV reports FOUND, times out, is unreachable or returns malformed output | Upload fails closed; no plaintext/unscanned fallback and no accepted evidence metadata |
| EVD-07 | Reporter requests another reporter's report/evidence | Denied; no metadata or bytes disclosed; denial audit event recorded |
| EVD-08 | Tenant A reviewer accesses tenant B report/evidence | Denied even with a valid reviewer grant in tenant A; denial audit event recorded |
| EVD-09 | Operator without `incident:review`, or support-only actor, requests evidence | Denied; role name alone does not confer reviewer authority |
| EVD-10 | Active same-tenant reviewer lists/downloads evidence | Allowed and attributed to the authenticated reviewer in the append-only access audit |
| EVD-11 | Successful and denied list/download attempts | Audit records actor, tenant, report/evidence identifiers, action, outcome, reason category and timestamp; no bytes/secrets |
| EVD-12 | Inspect private volume and database | Stored object is encrypted; filesystem root/object permissions are private; DB stores metadata, not evidence bytes |
| EVD-13 | Restart API and repeat authorized retrieval | Evidence remains available and decrypts correctly; no dependence on process-local state |
| EVD-14 | Add a new key ID while retaining the historical key | Existing objects remain decryptable; new writes use the active key ID |
| EVD-15 | Remove a required historical key in a disposable test | Retrieval fails closed and emits no plaintext; restore the key ring before continuing |
| EVD-16 | Configured retention expiry is reached | Retrieval is denied. **Current implementation does not delete bytes**; record this as a known limitation, not a pass for deletion/retention lifecycle |
| EVD-17 | Legal hold and deletion administration | **Not currently implemented.** Do not mark this scenario passed or claim operator-managed legal hold, deletion or backup expiry |
| EVD-18 | Restore database, encrypted evidence volume and key ring together | Restored evidence decrypts and authorization/audit metadata remains consistent; record restore duration and observed data loss |
| EVD-19 | Restore without the matching key ring | Retrieval fails closed; document the unrecoverable-data consequence and recovery procedure |
| EVD-20 | Inspect logs, metrics and error responses | No evidence bytes, original filenames, storage paths, encryption keys or credentials leak |

## Required evidence bundle

1. Exact Git commit SHA and immutable API/Web image digests.
2. Sanitized test run with scenario IDs, timestamps and pass/fail outcomes.
3. Redacted API request/response evidence and relevant audit-row identifiers.
4. Filesystem permission and encrypted-object verification (do not attach key material or plaintext).
5. ClamAV clean, infected, unavailable and timeout test evidence.
6. Key rotation and database + volume + key-ring restore rehearsal results.
7. Explicit list of untested scenarios, environment limitations and residual risks.

## Acceptance rules

- The authorized maintainer records the identity, exact commit SHA, environment and evidence reviewed. The maintainer may also be the implementation author; acceptance depends on reproducible scenario evidence and review of the results, not separation of human identities. Where an isolated deployment or disposable tenants are required by a scenario, preserve those technical conditions.
- A green CI run is necessary but does not satisfy this matrix or set `feature_list.json` `passes: true`.
- EVD-17 and the deletion portion of EVD-16 remain blocked until an audited retention/legal-hold/deletion workflow and backup-expiry coordination are implemented and tested.
- TRUST-002 remains open until all required implementation, exact-head checks and maintainer acceptance evidence are complete.
- Do not use real customer data or production evidence in this test plan.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.

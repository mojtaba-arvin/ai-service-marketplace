# Dispute Triage boundary

This package creates an evidence-grounded case summary, established-fact list, missing-information checklist and possible next-step options for a human adjudicator.

It is strictly advisory:
- Every result sets requires_human_adjudication=true.
- No outcome, refund amount, settlement instruction, liability determination or order mutation is part of the output schema; unknown authority-bearing fields are rejected.
- User and retrieved evidence text are redacted before model invocation.
- Every option cites only evidence returned by the market/locale/dispute-type scoped evidence source.
- Output is bounded, strict JSON with model/version and input/evidence provenance.
- Retrieval, redaction or model failure is fail-closed.

The package is a backend boundary only. Case persistence, API/UI integration, evidence upload/retention policy and independent adjudicator workflow remain separate work. No retention policy is inferred.

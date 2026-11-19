# Write a Specification and Acceptance Tests

## When
A feature or behavior is not fully specified.

## Steps
1. Identify user, business outcome, owning module, and dependencies.
2. Write observable acceptance criteria, including negative and failure cases.
3. Define API/event/schema contracts and authorization requirements.
4. Add tests that fail against the current implementation.
5. Document out-of-scope behavior, migration implications, and operational signals.
6. Keep the issue small enough for one focused implementation session.

## Verify
Run the relevant tests and `make verify`. A spec is ready only when criteria are testable and unresolved decisions are called out.

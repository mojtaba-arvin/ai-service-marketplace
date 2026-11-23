# Dynamic service form validation

This package validates versioned, admin-defined service questionnaires and submitted answers. It implements a deliberately bounded JSON Schema subset rather than evaluating arbitrary schema features.

Supported types: object, string, integer, number, boolean and array. Supported constraints: required fields, closed objects, enum, string length, numeric bounds, array item bounds and nested item/property schemas. Unsupported keywords are rejected during definition decoding.

Safety limits: 32 KiB definitions, 16 KiB answers, depth 8, 64 properties per object, 128 total fields, 100 array items and 32 enum values. Additional object properties are rejected. Definitions carry service code, version and locale so persistence and order snapshots can bind to an exact immutable version.

This package does not persist/publish definitions or mutate catalog/order records. Publication authorization, version lifecycle, API endpoints and immutable order answer snapshots remain integration work.

Run go test ./internal/serviceforms from apps/api.

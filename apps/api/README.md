# Core API

Go modular monolith for transactional marketplace capabilities. Domain modules own their internals and expose application interfaces and versioned events only. Keep transport, application, domain, and infrastructure concerns separate. Do not add a framework or dependency without a concrete requirement and ADR-level rationale.

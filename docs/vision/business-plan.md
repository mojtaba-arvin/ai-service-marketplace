# Business Plan — Managed Home Services Marketplace

## Executive summary
Build a two-sided, multi-market marketplace connecting households and organizations with vetted independent service professionals. The intended positioning is a **managed home-services marketplace**: trust, quality standards, transparent pricing, protected payments, repeat-service relationships, and operational tooling are core product capabilities—not optional add-ons.

The software can be architected for a complete product from the outset. Commercial rollout must remain gated by city, service category, provider density, legal readiness, and operational capacity. A complete architecture is not a reason to launch every market or service simultaneously.

## Business model
Potential revenue streams:
- Commission on completed in-platform transactions; the initial 15–25% range is a hypothesis to validate, not a committed rate.
- Customer subscriptions and recurring maintenance/cleaning plans.
- Professional business tools (calendar, invoicing, customer history, reporting).
- B2B property/building maintenance contracts and consolidated billing.
- Optional extended guarantees and other clearly priced value-added services.

## Core marketplace risks
1. **Supply density:** insufficient qualified professionals in a local service area increases matching time and abandonment.
2. **Trust and safety:** providers enter private homes; identity, credential review, incident response, evidence handling, and human escalation matter.
3. **Disintermediation:** repeat relationships can move off-platform. Retention should come from protected payments, service guarantees, recurring plans, service history, and professional tools—not punitive dark patterns.
4. **Pricing ambiguity:** fixed menus suit standardized jobs; uncertain work needs structured inspection and customer-approved quotes/variations.
5. **Unit economics:** acquisition cost, repeat rate, average order value, provider earnings, refunds, payment fees, support load, and contribution margin must be measured by market/category.
6. **Regulation:** worker classification, licenses, insurance, KYC, consumer protection, tax, and payment rules differ by jurisdiction.

## Initial operating strategy
The proposed first market should open only after local legal and operational validation. Candidate categories include recurring cleaning, HVAC servicing, basic plumbing, and electrical work. Category selection must consider frequency, job value, safety exposure, provider availability, and repeat potential.

Use market configuration and feature gates to limit public availability by market, region, category, and capability. This is a controlled rollout of a complete platform, not a reduction of architectural scope.

## Pricing principles
- Fixed, versioned price menus for standardized work.
- Structured quote workflow for unknown scope.
- Explicit customer approval for price or scope changes.
- Store all amounts in integer minor units with currency and pricing-version references.
- AI may classify a request and suggest a range with confidence and provenance; deterministic rules and customer approval govern payable amounts.

## Trust and retention
- Layered provider verification and skill checks, with market-specific legal review.
- In-platform communication and privacy-preserving contact workflows where lawful.
- Clear cancellation, refund, dispute, guarantee, and evidence policies.
- Recurring service plans and durable service history.
- Explainable quality signals; no opaque automated punishment.

## North-star and health metrics
North-star: completed orders without unresolved issues per week.

Track demand conversion and CAC; active qualified supply and time-to-accept; completion, lateness, cancellation, rework, disputes, and customer satisfaction; take rate and contribution margin; repeat purchase and retention; provider earnings and utilization; and suspected off-platform leakage. Define each metric's event source and denominator before reporting it.

## Research discipline
All market sizing, competitor pricing, commission benchmarks, provider economics, and regulatory claims must be backed by dated primary or credible secondary sources and tagged with confidence. Treat values in the initial proposal as hypotheses until field research validates them. Store findings in `docs/research/` with sources and dates. R&D agents may propose changes but must never directly change live prices, compliance rules, or market availability.

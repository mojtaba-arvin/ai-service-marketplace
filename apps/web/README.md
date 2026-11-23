# Marketplace Web

Responsive Next.js customer-facing application within the complete multi-market marketplace. The shared browser client currently exposes a subset of customer and professional workflows; the full adopter-facing scope remains M1–M14 in the root product catalogue.

## Workflows

- OTP sign-in through the configured API delivery provider.
- Customer service discovery, scheduled booking, order history, and policy-based cancellation.
- Professional profile registration, service-area and availability submission, offer acceptance, and order lifecycle updates.
- Operator order queue, deterministic dispatch, and professional verification.

The browser client calls the same-origin `/v1` API. Caddy routes `/v1/*` to the Go API and all other requests to this Next.js application.

## Local development

```sh
npm install
npm run dev
```

The API must be available through the same origin at `/v1`, or use a local reverse proxy. Production uses the standalone container in `Dockerfile`.

## Security boundary

Browser sessions use Secure, HttpOnly access/refresh cookies with a separate same-origin CSRF token. The client does not persist bearer credentials in browser storage. Payment credentials and funds-moving actions are not exposed in the current browser workflow; payment integrations remain separately gated.

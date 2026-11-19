# Marketplace Web

Responsive Next.js application for the single-market MVP. The app provides a shared browser experience for customers, professionals, and marketplace operators.

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

Access and refresh tokens are kept in browser session storage for this MVP. This is a deliberate temporary boundary, not the final production session design. Before broad public launch, move session handling to Secure, HttpOnly, SameSite cookies and add a reviewed CSRF strategy. No payment credentials or payment actions are exposed in this client.

# Changelog

Sample changelog for the demo project. The Status page reads its releases and the entries under `## Unreleased`.

## Unreleased

- **Distributed tracing.** The API, the payment service and the workers export OpenTelemetry traces to the collector in every environment.
- **Database migrations.** Versioned up/down migrations run on deploy, with a dry-run mode for review.
- **JWT auth middleware.** Every API route validates a signed token; expired and revoked tokens get `401`.

## v0.9.0 — 2026-10-08

- **User accounts and sign-in.** Sign-up, sign-in and password reset by email.
- **Account settings page.** Change email, password and display name.
- **Staging environment.** Rebuilt from main on every merge.

## v0.8.0 — 2026-09-12

- **Public API preview.** Read-only endpoints for projects and issues behind an API key.

# 1. First-party JWT authentication

- Status: Accepted
- Date: 2026-11-18

## Context

The reservation API needs a real user identity boundary. It currently trusts a client-supplied `customer_id`, which does not prove who is making a request. An external identity provider would add configuration and hide the password/token fundamentals this portfolio project demonstrates.

## Decision

Store users in PostgreSQL with normalized lowercase email addresses and bcrypt password hashes. Issue HS256 access tokens with a 15-minute expiry after registration/login, validating issuer, audience, subject, issued-at, expiry, and algorithm on every protected request. Only the API receives `JWT_SECRET`. Reservation creation and status reads derive or check the customer UUID from the token subject rather than trusting request input.

## Consequences

- Protected routes cannot be called without a valid bearer token; health and readiness remain public for orchestration.
- Passwords are never logged or returned; bcrypt's 72-byte input limit is enforced explicitly.
- Tokens cannot be revoked before expiry because there is no session store or blacklist.
- Production must supply `JWT_SECRET` through VM-local environment configuration, never source control.
- Existing reservations with arbitrary customer UUIDs remain readable only by a matching user record; no backfill is attempted in this migration.

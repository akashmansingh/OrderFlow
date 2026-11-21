# JWT Authentication Design

## Intent

Add first-party authentication to OrderFlow so the reservation API has a real user identity boundary. Users register with an email and password, log in to receive a short-lived access token, and the API uses that token's subject as the reservation customer ID.

## Approaches considered

1. **PostgreSQL users, bcrypt, and signed JWT access tokens — selected.** Demonstrates password handling, authentication endpoints, middleware, token validation, and protected API ownership.
2. **Static development token.** Faster but has no real user model or login flow.
3. **External OIDC provider.** Defers password handling but adds external configuration and hides the fundamentals this project is meant to demonstrate.

## Selected design

### User data

- Add a `users` table with UUID `id`, normalized lowercase `email`, bcrypt `password_hash`, and timestamps.
- Enforce email uniqueness with a database unique constraint.
- Normalize email by trimming whitespace and lowercasing before storage and login lookup.
- Passwords are never logged, returned, or stored in plaintext.

### Authentication API

- `POST /v1/auth/register` accepts `email` and `password`, creates one user, and returns `201` with the user ID and normalized email. Duplicate email returns `409`.
- `POST /v1/auth/login` accepts the same fields and returns an access token, token type `Bearer`, and expiry timestamp. Invalid credentials return `401` without revealing whether the email exists.
- Password rules: at least 12 characters. No refresh token, password reset, email verification, or external identity provider in this increment.

### JWTs and middleware

- Use HS256 with `JWT_SECRET`, a required 32-byte-or-longer secret supplied through ignored `.env` files or deployment secrets.
- Tokens contain the user UUID in `sub`, plus issuer `orderflow`, audience `orderflow-api`, issued-at, and a 15-minute expiry. Validate all of those claims and reject algorithms other than HS256.
- Health and readiness routes remain public. All `/v1` event and reservation routes require `Authorization: Bearer <token>`.
- Reservation creation derives `customer_id` from the authenticated token subject; the client no longer supplies it. Reservation status reads require the same customer subject. Event creation/read remains authenticated but has no role model yet.

### Operational behavior

- Add `JWT_SECRET`, `JWT_ACCESS_TOKEN_MINUTES`, `JWT_ISSUER`, and `JWT_AUDIENCE` to configuration, `.env.example` placeholders, Compose, local-development documentation, and the README quick-start flow.
- The Docker stack requires a local generated development secret. Production uses a VM-local `.env`; the secret never goes into GitHub source or commits.

## Verification

- Test password validation/hash behavior, token creation/validation, expired/invalid token rejection, login credential failure, and protected-route middleware behavior.
- Add opt-in integration tests for registration/login and reservation ownership; do not run live E2E tests unless explicitly requested.

## Non-goals

- No refresh tokens, password reset, email verification, MFA, roles, RBAC, OAuth/OIDC, session revocation, or distributed token blacklist.

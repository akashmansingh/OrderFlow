# OrderFlow web

Frontend for the OrderFlow reservation backend. Vite, React 19, TypeScript and Tailwind 4. Animation uses the Motion library, and the only other runtime dependencies are the three Fontsource font packages.

## Run it

Against the real backend (docker compose from the OrderFlow repo, API on `:8080`):

```bash
cp .env.example .env
npm install
npm run dev          # http://localhost:5173
```

The dev server proxies `/api` to `VITE_API_TARGET`, so the browser never makes a cross-origin call and can read the `traceparent` response header.

Without Docker, against a mock that follows the same API contract:

```bash
npm run demo         # mock API on :8787 + web on :5173
```

Mock knobs: `MOCK_PAYMENT=approve|reject|random|stall`, `MOCK_HOLD_SECONDS`, `MOCK_SETTLE_MS`.

## Screens

| Component | What it does |
| --- | --- |
| `AuthModal` | Register and sign-in tabs. Stores the JWT, sends `Authorization: Bearer`, signs out on a 401 or when the token expires. |
| `EventList` / `EventCard` | Events as tickets. The stub shows live availability (polled every 2.5s) and flips when it changes. `Create demo event` adds Coldplay Live (50) and others. |
| `ReservationModal` | Quantity 1 to 10, one `Idempotency-Key` (UUID v4) per checkout session, reused on retries. Handles 409 (sold out) and 429 (countdown from `Retry-After`). |
| `ReservationStatus` | 10-minute hold countdown, Held / Payment / Result stepper, status stamp. Polls `GET /v1/reservations/{id}` every 1.5s until it reaches a terminal state, and resumes after a refresh. |
| `TraceInspector` | Idempotency key, `traceparent`, parsed trace ID, link to `{JAEGER_URL}/trace/{traceId}`, Grafana link, and a log of recent API calls. |

## Backend assumptions to check

- `GET /v1/events` returns an array (or `{events: [...]}`). If it omits `available`, the client reads each event individually.
- The reservation response carries `expires_at` if possible. If it doesn't, the countdown assumes a 10-minute hold from the time of reservation.
- A direct (non-proxied) API origin needs CORS with `Access-Control-Expose-Headers: traceparent, Retry-After`.

## Checks

```bash
npm run typecheck
npm run build
```

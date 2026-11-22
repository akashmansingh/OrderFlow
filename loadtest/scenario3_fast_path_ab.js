import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const successfulReservations = new Counter('successful_reservations');
const soldOutRejections = new Counter('sold_out_rejections');
const errorRate = new Rate('unexpected_errors');
const soldOutLatency = new Trend('sold_out_latency_ms');

const tokens = JSON.parse(open('./tokens.json'));

export const options = {
  scenarios: {
    fast_path_contention: {
      executor: 'constant-arrival-rate',
      rate: 1000,
      timeUnit: '1s',
      duration: '10s',
      preAllocatedVUs: 200,
      maxVUs: 1000,
    },
  },
  thresholds: {
    'unexpected_errors': ['rate<0.01'],
  },
};

const BASE_URL = __ENV.TARGET_URL || 'http://localhost:8080';

export function setup() {
  const adminToken = tokens[0].token;
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${adminToken}`,
  };

  const startsAt = new Date(Date.now() + 86400000).toISOString();
  // Small capacity: 50 units so the event rapidly sells out and 95%+ of requests exercise the sold-out path
  const payload = JSON.stringify({
    name: `Fast-Path A/B Event - ${Date.now()}`,
    starts_at: startsAt,
    capacity: 50,
  });

  const res = http.post(`${BASE_URL}/v1/events`, payload, { headers });
  check(res, { 'event created': (r) => r.status === 201 });
  const event = res.json();
  console.log(`Setup created Fast-Path A/B Event: ID=${event.id}, Capacity=50`);
  return { eventId: event.id };
}

export default function (data) {
  const user = tokens[__VU % tokens.length];
  const idempotencyKey = `fast-path-ab-${__VU}-${__ITER}-${Date.now()}-${Math.random()}`;
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
    'Idempotency-Key': idempotencyKey,
  };

  const payload = JSON.stringify({ quantity: 1 });
  const start = Date.now();
  const res = http.post(`${BASE_URL}/v1/events/${data.eventId}/reservations`, payload, { headers });
  const elapsed = Date.now() - start;

  if (res.status === 201) {
    successfulReservations.add(1);
  } else if (res.status === 409) {
    soldOutRejections.add(1);
    soldOutLatency.add(elapsed);
  } else {
    errorRate.add(1);
    check(res, { 'unexpected status': (r) => r.status === 201 || r.status === 409 });
  }
}

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const successfulReservations = new Counter('successful_reservations');
const soldOutRejections = new Counter('sold_out_rejections');
const errorRate = new Rate('unexpected_errors');
const latencyTrend = new Trend('soak_latency_ms');

const tokens = JSON.parse(open('./tokens.json'));

export const options = {
  scenarios: {
    steady_soak: {
      executor: 'constant-arrival-rate',
      rate: 200,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 50,
      maxVUs: 200,
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
  const payload = JSON.stringify({
    name: `Soak Test Event - ${Date.now()}`,
    starts_at: startsAt,
    capacity: 20000,
  });

  const res = http.post(`${BASE_URL}/v1/events`, payload, { headers });
  check(res, { 'event created': (r) => r.status === 201 });
  const event = res.json();
  console.log(`Setup created Soak Event: ID=${event.id}, Capacity=20000`);
  return { eventId: event.id };
}

export default function (data) {
  const user = tokens[__VU % tokens.length];
  const idempotencyKey = `soak-${__VU}-${__ITER}-${Date.now()}-${Math.random()}`;
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
    'Idempotency-Key': idempotencyKey,
  };

  const start = Date.now();
  const res = http.post(`${BASE_URL}/v1/events/${data.eventId}/reservations`, JSON.stringify({ quantity: 1 }), { headers });
  latencyTrend.add(Date.now() - start);

  if (res.status === 201) {
    successfulReservations.add(1);
  } else if (res.status === 409) {
    soldOutRejections.add(1);
  } else {
    errorRate.add(1);
  }
}

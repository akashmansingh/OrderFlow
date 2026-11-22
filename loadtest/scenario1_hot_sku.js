import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const successfulReservations = new Counter('successful_reservations');
const soldOutRejections = new Counter('sold_out_rejections');
const errorRate = new Rate('unexpected_errors');
const reservationLatency = new Trend('reservation_latency_ms');

// Load pre-generated test user tokens
const tokens = JSON.parse(open('./tokens.json'));

export const options = {
  scenarios: {
    hot_sku_burst: {
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
  const createPayload = JSON.stringify({
    name: `Hot SKU Concert - ${Date.now()}`,
    starts_at: startsAt,
    capacity: 100,
  });

  const res = http.post(`${BASE_URL}/v1/events`, createPayload, { headers });
  check(res, {
    'event created successfully': (r) => r.status === 201,
  });

  const event = res.json();
  console.log(`Setup created Hot SKU Event: ID=${event.id}, Capacity=100`);
  return { eventId: event.id };
}

export default function (data) {
  const user = tokens[__VU % tokens.length];
  const idempotencyKey = `hot-sku-${__VU}-${__ITER}-${Date.now()}-${Math.random()}`;

  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
    'Idempotency-Key': idempotencyKey,
  };

  const payload = JSON.stringify({
    quantity: 1,
  });

  const start = Date.now();
  const res = http.post(`${BASE_URL}/v1/events/${data.eventId}/reservations`, payload, { headers });
  reservationLatency.add(Date.now() - start);

  if (res.status === 201) {
    successfulReservations.add(1);
    check(res, { 'status is 201': (r) => r.status === 201 });
  } else if (res.status === 409) {
    soldOutRejections.add(1);
    check(res, { 'status is 409 sold out': (r) => r.status === 409 });
  } else {
    errorRate.add(1);
    check(res, { 'unexpected status': (r) => r.status === 201 || r.status === 409 });
  }
}

export function teardown(data) {
  console.log(`Completed Hot SKU run for Event ID: ${data.eventId}`);
}

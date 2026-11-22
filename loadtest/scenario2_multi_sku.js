import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const successfulReservations = new Counter('successful_reservations');
const soldOutRejections = new Counter('sold_out_rejections');
const errorRate = new Rate('unexpected_errors');
const reservationLatency = new Trend('reservation_latency_ms');

const tokens = JSON.parse(open('./tokens.json'));

export const options = {
  scenarios: {
    multi_sku_spread: {
      executor: 'constant-arrival-rate',
      rate: 800,
      timeUnit: '1s',
      duration: '15s',
      preAllocatedVUs: 150,
      maxVUs: 800,
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

  const eventCount = 10;
  const eventIds = [];
  const startsAt = new Date(Date.now() + 86400000).toISOString();

  for (let i = 0; i < eventCount; i++) {
    const capacity = i === 0 ? 50 : 200; // Event 0 is hot/scarce, others have higher capacity
    const payload = JSON.stringify({
      name: `Multi-SKU Event ${i + 1} - ${Date.now()}`,
      starts_at: startsAt,
      capacity: capacity,
    });

    const res = http.post(`${BASE_URL}/v1/events`, payload, { headers });
    check(res, { 'event created': (r) => r.status === 201 });
    eventIds.push(res.json().id);
  }

  console.log(`Setup created ${eventIds.length} events across Zipf distribution.`);
  return { eventIds };
}

// Zipf-like index selection (skew towards index 0)
function selectZipfIndex(n) {
  const r = Math.random();
  // Simple power-law mapping: index 0 gets ~50% of traffic, index 1 gets ~25%, etc.
  if (r < 0.50) return 0;
  if (r < 0.75) return 1;
  if (r < 0.88) return 2;
  return Math.floor(Math.random() * (n - 3)) + 3;
}

export default function (data) {
  const user = tokens[__VU % tokens.length];
  const targetIndex = selectZipfIndex(data.eventIds.length);
  const targetEventId = data.eventIds[targetIndex];

  const idempotencyKey = `zipf-${__VU}-${__ITER}-${Date.now()}-${Math.random()}`;
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
    'Idempotency-Key': idempotencyKey,
  };

  const payload = JSON.stringify({ quantity: 1 });
  const start = Date.now();
  const res = http.post(`${BASE_URL}/v1/events/${targetEventId}/reservations`, payload, { headers });
  reservationLatency.add(Date.now() - start);

  if (res.status === 201) {
    successfulReservations.add(1);
  } else if (res.status === 409) {
    soldOutRejections.add(1);
  } else {
    errorRate.add(1);
    check(res, { 'unexpected status': (r) => r.status === 201 || r.status === 409 });
  }
}

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const reservationsAttempted = new Counter('reservations_attempted');
const successfulReservations = new Counter('successful_reservations');
const soldOutRejections = new Counter('sold_out_rejections');
const serverErrors = new Counter('server_errors');
const reservationLatency = new Trend('reservation_latency_ms');

const tokens = JSON.parse(open('./tokens.json'));

export const options = {
  scenarios: {
    chaos_traffic: {
      executor: 'constant-arrival-rate',
      rate: 300,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 100,
      maxVUs: 500,
    },
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
    name: `Chaos Event - ${Date.now()}`,
    starts_at: startsAt,
    capacity: 500,
  });

  const res = http.post(`${BASE_URL}/v1/events`, payload, { headers });
  check(res, { 'event created': (r) => r.status === 201 });
  const event = res.json();
  console.log(`Setup created Chaos Event: ID=${event.id}, Capacity=500`);
  return { eventId: event.id };
}

export default function (data) {
  const user = tokens[__VU % tokens.length];
  const idempotencyKey = `chaos-${__VU}-${__ITER}-${Date.now()}-${Math.random()}`;
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
    'Idempotency-Key': idempotencyKey,
  };

  reservationsAttempted.add(1);
  const start = Date.now();
  const res = http.post(`${BASE_URL}/v1/events/${data.eventId}/reservations`, JSON.stringify({ quantity: 1 }), { headers });
  reservationLatency.add(Date.now() - start);

  if (res.status === 201) {
    successfulReservations.add(1);
  } else if (res.status === 409) {
    soldOutRejections.add(1);
  } else if (res.status >= 500) {
    serverErrors.add(1);
  }
}

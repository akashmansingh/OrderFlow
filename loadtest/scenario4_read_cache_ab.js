import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const readSuccess = new Counter('read_success_total');
const errorRate = new Rate('unexpected_errors');
const readLatency = new Trend('read_latency_ms');

const tokens = JSON.parse(open('./tokens.json'));

export const options = {
  scenarios: {
    read_heavy_traffic: {
      executor: 'constant-arrival-rate',
      rate: 1500,
      timeUnit: '1s',
      duration: '10s',
      preAllocatedVUs: 200,
      maxVUs: 1000,
    },
  },
  thresholds: {
    'unexpected_errors': ['rate<0.005'],
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
    name: `Read Benchmark Event - ${Date.now()}`,
    starts_at: startsAt,
    capacity: 10000,
  });

  const res = http.post(`${BASE_URL}/v1/events`, payload, { headers });
  check(res, { 'event created': (r) => r.status === 201 });
  const event = res.json();
  console.log(`Setup created Read Benchmark Event: ID=${event.id}`);
  return { eventId: event.id };
}

export default function (data) {
  const user = tokens[__VU % tokens.length];
  const headers = {
    'Authorization': `Bearer ${user.token}`,
  };

  const start = Date.now();
  const res = http.get(`${BASE_URL}/v1/events/${data.eventId}`, { headers });
  readLatency.add(Date.now() - start);

  if (res.status === 200) {
    readSuccess.add(1);
    check(res, { 'status is 200': (r) => r.status === 200 });
  } else {
    errorRate.add(1);
  }
}

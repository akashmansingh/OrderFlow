-- OrderFlow Showcase Demo Seeder
-- Resets benchmark/test artifacts while preserving registered users

TRUNCATE TABLE idempotency_keys, reservations, events CASCADE;

INSERT INTO events (id, name, starts_at, capacity, held_quantity, confirmed_quantity) VALUES
  (gen_random_uuid(), 'Coldplay Live 2026', NOW() + INTERVAL '14 days', 50, 0, 0),
  (gen_random_uuid(), 'Taylor Swift | The Eras Tour', NOW() + INTERVAL '21 days', 20, 0, 0),
  (gen_random_uuid(), 'Midnight Jazz Cellar', NOW() + INTERVAL '7 days', 30, 0, 0),
  (gen_random_uuid(), 'Lakeside Film Night', NOW() + INTERVAL '28 days', 100, 0, 0);

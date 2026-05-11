CREATE TABLE IF NOT EXISTS users (
  id integer PRIMARY KEY,
  email text NOT NULL,
  plan text NOT NULL DEFAULT 'free'
);

CREATE TABLE IF NOT EXISTS orders (
  id integer PRIMARY KEY,
  user_id integer NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  total_cents integer NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id integer PRIMARY KEY,
  message text NOT NULL
);

INSERT INTO users (id, email, plan) VALUES
  (1, 'ada@example.com', 'pro'),
  (2, 'grace@example.com', 'free')
ON CONFLICT (id) DO NOTHING;

INSERT INTO orders (id, user_id, total_cents) VALUES
  (1, 1, 1200),
  (2, 2, 800)
ON CONFLICT (id) DO NOTHING;

INSERT INTO audit_logs (id, message) VALUES
  (1, 'created demo user'),
  (2, 'updated demo plan')
ON CONFLICT (id) DO NOTHING;

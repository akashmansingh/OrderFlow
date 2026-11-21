CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL CHECK (
        email = btrim(email)
        AND email = lower(email)
        AND position('@' IN email) > 1
    ),
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX users_email_key ON users (email);

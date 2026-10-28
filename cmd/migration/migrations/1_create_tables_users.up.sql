CREATE TABLE IF NOT EXISTS users(
    id uuid PRIMARY KEY,
    email VARCHAR(50) UNIQUE,
    password VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at timestamptz,
    deleted_at timestamptz 
);
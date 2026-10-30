CREATE TABLE IF NOT EXISTS users(
    id uuid PRIMARY KEY,
    email VARCHAR(50) UNIQUE,
    password VARCHAR(255),
    active BOOLEAN NOT NULL,
    role_id smallint NOT NULL,
    created_at timestamptz  NOT NULL,
    updated_at timestamptz,
    deleted_at timestamptz 
);
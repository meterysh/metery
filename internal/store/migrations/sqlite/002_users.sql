-- +goose Up

CREATE TABLE users (
  id         TEXT PRIMARY KEY,
  google_id  TEXT NOT NULL UNIQUE,
  email      TEXT NOT NULL,
  name       TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT current_timestamp
);

-- +goose Down
DROP TABLE users;

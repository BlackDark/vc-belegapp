-- +goose Up
CREATE TABLE system (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE system;

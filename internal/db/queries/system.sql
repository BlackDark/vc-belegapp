-- name: GetSystemValue :one
SELECT value FROM system WHERE key = ?;

-- name: UpsertSystemValue :exec
INSERT INTO system (key, value)
VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

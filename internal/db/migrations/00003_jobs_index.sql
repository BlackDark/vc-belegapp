-- +goose Up
CREATE INDEX jobs_bereit ON jobs (status, naechster_versuch_am);

-- +goose Down
DROP INDEX jobs_bereit;

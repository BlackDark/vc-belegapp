-- The prune in internal/jobs reads finished rows by age: "everything older
-- than BELEGAPP_JOB_RETENTION". jobs_bereit (00003) leads with status and
-- orders by naechster_versuch_am, so it can find the rows but cannot order
-- them by the timestamp the window is measured against and still stop early.
-- A partial index over exactly the two statuses the prune deletes answers the
-- query in order and lets it stop at the batch limit, so the cost of a prune
-- is proportional to what it removes rather than to the whole history.
--
-- The index is partial on purpose: wartend and laeuft rows are never deleted,
-- and they are the ones that get updated most often, so indexing them would
-- buy nothing and cost on every claim.
-- +goose Up
CREATE INDEX jobs_fertig_geaendert_am ON jobs (geaendert_am)
WHERE status IN ('fertig', 'fehler');

-- +goose Down
DROP INDEX jobs_fertig_geaendert_am;
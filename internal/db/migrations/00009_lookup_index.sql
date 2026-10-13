-- Three lookups the queue runs on every poll or every image delete, and two of
-- them run while a person is waiting.
--
-- 1. CountBlobKey is "does anything still name this blob", asked twice per
--    deleteBlobIfFree and twice per DeleteBild — so one image delete walked
--    belegbilder four times. Neither column was indexed. With statistics, on
--    the schema before this migration:
--      SCAN belegbilder
--    With the two indexes below and the same statistics:
--      MULTI-INDEX OR
--         INDEX 1 SEARCH belegbilder USING INDEX belegbilder_blob_key (blob_key=?)
--         INDEX 2 SEARCH belegbilder USING INDEX belegbilder_thumb_blob_key (thumb_blob_key=?)
-- 2. CountOpenJobsByPayload is "is a poll of mine already queued", asked on
--    every mail tick (5 min) and every watched-folder tick (30 s). The index
--    that used to cover it is jobs_bereit (status, naechster_versuch_am), and
--    with statistics the plan is
--      SCAN jobs
--    — which is the measured reason this migration exists. Without statistics
--    the same question was at least a seek into the waiting rows,
--      SEARCH jobs USING INDEX jobs_bereit (status=?)
--    and ANALYZE is what turned it into a scan: the statistics say the open
--    jobs are a small share of a table the prune deletes from hourly, so the
--    planner stops trusting that status is selective.
--
-- The new index is partial because the question only ever has an answer for a
-- job that still owes work. A finished row cannot suppress a poll, and
-- wartend/laeuft are the rows the queue updates most often, so indexing the
-- rest of the table would buy nothing and cost on every claim.
--
-- The indexes only become the plan the planner picks because something runs
-- ANALYZE, and the thing that runs it is the hourly prune (internal/jobs).
-- That is why the two land together: an index the planner never chooses is a
-- write cost with no read to show for it.
--
-- belegbilder_beleg from 00002 is left alone. It is a strict prefix of the
-- UNIQUE belegbilder_beleg_seite (beleg_id, seite), so it looks redundant, and
-- an earlier attempt at this migration dropped it on those grounds. Measured,
-- it does not help. ListUnassignedBilder reads
--   SEARCH belegbilder USING INDEX belegbilder_beleg (beleg_id=?)
--   USE TEMP B-TREE FOR ORDER BY
-- and with belegbilder_beleg dropped the same query reads
--   SEARCH belegbilder USING INDEX belegbilder_beleg_seite (beleg_id=?)
--   USE TEMP B-TREE FOR ORDER BY
-- — one index over, the same plan. The ordered partial index from 00004 is
-- still not chosen; the planner only takes it when forced (INDEXED BY), where
-- it costs a scan of it. So dropping it buys no plan and costs a rebuild of
-- every belegbilder table on the next upgrade. A hot index needs a measured
-- reason to go, and there is not one here.
-- +goose Up
CREATE INDEX belegbilder_blob_key ON belegbilder (blob_key);
CREATE INDEX belegbilder_thumb_blob_key ON belegbilder (thumb_blob_key);

CREATE INDEX jobs_offen_typ_payload ON jobs (typ, payload)
WHERE status IN ('wartend', 'laeuft');

-- +goose Down
DROP INDEX jobs_offen_typ_payload;
DROP INDEX belegbilder_thumb_blob_key;
DROP INDEX belegbilder_blob_key;
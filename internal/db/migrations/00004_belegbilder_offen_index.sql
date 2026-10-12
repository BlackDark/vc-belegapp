-- The "offene Bilder" list and the sweeper both read the unassigned rows.
-- belegbilder_beleg_seite serves the filter but not the ordering, so every
-- list call sorted the whole table before it could page. This partial index
-- covers the filter and the ORDER BY for exactly those rows.
-- +goose Up
CREATE INDEX belegbilder_offen_erstellt_am ON belegbilder (erstellt_am DESC, id DESC)
WHERE beleg_id IS NULL;

-- +goose Down
DROP INDEX belegbilder_offen_erstellt_am;
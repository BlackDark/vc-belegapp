-- The watched-folder intake runs as a queue job, so jobs.typ needs a fifth
-- value. SQLite cannot ALTER a CHECK constraint, so the table is rebuilt:
-- copy every row into the new shape, drop the old table, rename, and recreate
-- jobs_bereit from 00003, which the DROP took with it.
--
-- There are no foreign keys on jobs and nothing references it, and the two
-- CHECKs that survive the rebuild (status, versuche >= 0 by NOT NULL DEFAULT)
-- are unchanged, so the copy is lossless. internal/db/migrate_test.go asserts
-- that rows of all four old types survive with every column intact.
-- +goose Up
CREATE TABLE jobs_watch (
    id TEXT PRIMARY KEY,
    typ TEXT NOT NULL CHECK (typ IN ('erkennung', 'datenexport', 'mail', 'notify', 'watch')),
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('wartend', 'laeuft', 'fertig', 'fehler')),
    versuche INTEGER NOT NULL DEFAULT 0,
    naechster_versuch_am TEXT NOT NULL,
    ergebnis TEXT NULL,
    fehler TEXT NULL,
    erstellt_am TEXT NOT NULL,
    geaendert_am TEXT NOT NULL
);

INSERT INTO jobs_watch (
    id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
) SELECT
    id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
FROM jobs;

DROP TABLE jobs;

ALTER TABLE jobs_watch RENAME TO jobs;

CREATE INDEX jobs_bereit ON jobs (status, naechster_versuch_am);

-- +goose Down
-- Reverting the CHECK would fail on the watch rows. A watch row is one poll of
-- a folder, and the next poll asks the folder again, so dropping those rows
-- loses no user data; every other row is copied back unchanged.
CREATE TABLE jobs_no_watch (
    id TEXT PRIMARY KEY,
    typ TEXT NOT NULL CHECK (typ IN ('erkennung', 'datenexport', 'mail', 'notify')),
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('wartend', 'laeuft', 'fertig', 'fehler')),
    versuche INTEGER NOT NULL DEFAULT 0,
    naechster_versuch_am TEXT NOT NULL,
    ergebnis TEXT NULL,
    fehler TEXT NULL,
    erstellt_am TEXT NOT NULL,
    geaendert_am TEXT NOT NULL
);

INSERT INTO jobs_no_watch (
    id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
) SELECT
    id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
FROM jobs
WHERE typ <> 'watch';

DROP TABLE jobs;

ALTER TABLE jobs_no_watch RENAME TO jobs;

CREATE INDEX jobs_bereit ON jobs (status, naechster_versuch_am);

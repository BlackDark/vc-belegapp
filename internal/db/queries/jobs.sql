-- name: InsertJob :exec
INSERT INTO jobs (
    id, typ, payload, status, versuche, naechster_versuch_am, erstellt_am, geaendert_am
) VALUES (
    ?, ?, ?, 'wartend', 0, ?, ?, ?
);

-- name: GetJob :one
SELECT id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
FROM jobs
WHERE id = ?;

-- name: NextWaitingJob :one
SELECT id, typ, payload, status, versuche, naechster_versuch_am, ergebnis, fehler, erstellt_am, geaendert_am
FROM jobs
WHERE status = 'wartend' AND naechster_versuch_am <= ?
ORDER BY naechster_versuch_am ASC, erstellt_am ASC
LIMIT 1;

-- name: ClaimJob :execresult
UPDATE jobs
SET status = 'laeuft',
    versuche = versuche + 1,
    geaendert_am = ?
WHERE id = ? AND status = 'wartend';

-- name: UpdateJob :exec
UPDATE jobs
SET status = ?,
    naechster_versuch_am = ?,
    ergebnis = ?,
    fehler = ?,
    geaendert_am = ?
WHERE id = ?;

-- name: ResetRunningJobs :exec
UPDATE jobs
SET status = 'wartend',
    geaendert_am = ?
WHERE status = 'laeuft';

-- name: CountJobsByStatus :one
SELECT COUNT(*) FROM jobs WHERE status = ?;

-- name: CountOpenJobsByPayload :one
SELECT COUNT(*) FROM jobs
WHERE typ = ? AND payload = ? AND status IN ('wartend', 'laeuft');

-- name: SetBelegbildErkennung :exec
UPDATE belegbilder
SET erkennung_status = ?,
    erkennung_ergebnis = ?,
    erkennung_roh = ?,
    erkennung_fehler = ?,
    erkennung_modell = ?,
    erkennung_dauer_ms = ?
WHERE id = ?;

-- name: SetErkennungStatus :exec
UPDATE belegbilder
SET erkennung_status = ?,
    erkennung_fehler = ?
WHERE id = ?;

-- name: SaveErkennungVersuch :exec
UPDATE belegbilder
SET erkennung_roh = ?,
    erkennung_modell = ?,
    erkennung_dauer_ms = ?
WHERE id = ?;

-- name: ResetLaufendeErkennung :exec
UPDATE belegbilder
SET erkennung_status = 'ausstehend'
WHERE erkennung_status = 'laeuft';

-- name: ListErkennungOffen :many
SELECT id FROM belegbilder
WHERE erkennung_status IN ('ausstehend', 'laeuft');

-- name: GetEinstellungen :one
SELECT id, arbeitnehmer_name, personalnummer, arbeitgeber_name, standard_bezugsort,
       standard_arbeitsort, erkennung_aktiv, export_zip_standard, export_csv_standard, geaendert_am
FROM einstellungen
WHERE id = 1;

-- name: UpdateEinstellungen :exec
UPDATE einstellungen
SET arbeitnehmer_name = ?,
    personalnummer = ?,
    arbeitgeber_name = ?,
    standard_bezugsort = ?,
    standard_arbeitsort = ?,
    erkennung_aktiv = ?,
    export_zip_standard = ?,
    export_csv_standard = ?,
    geaendert_am = ?
WHERE id = 1;

-- name: ListJahresregeln :many
SELECT * FROM jahresregeln ORDER BY jahr;

-- name: GetJahresregel :one
SELECT * FROM jahresregeln WHERE jahr = ?;

-- name: UpsertJahresregel :exec
INSERT INTO jahresregeln (
    jahr, zuschuss_cent, mahlzeiten, standard_mahlzeit,
    sbw_fruehstueck_cent, sbw_mittag_cent, sbw_abend_cent, hoechstzuschuss_aufschlag_cent,
    pauschalierung, pauschsteuersatz_bp, soli_satz_bp, gehaltsumwandlung,
    bundesland, kist_satz_bp, eigenanteil_variante, monatslimit, limit_modus,
    eigene_feiertage, notiz, erstellt_am, geaendert_am
) VALUES (
    ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?
)
ON CONFLICT (jahr) DO UPDATE SET
    zuschuss_cent = excluded.zuschuss_cent,
    mahlzeiten = excluded.mahlzeiten,
    standard_mahlzeit = excluded.standard_mahlzeit,
    sbw_fruehstueck_cent = excluded.sbw_fruehstueck_cent,
    sbw_mittag_cent = excluded.sbw_mittag_cent,
    sbw_abend_cent = excluded.sbw_abend_cent,
    hoechstzuschuss_aufschlag_cent = excluded.hoechstzuschuss_aufschlag_cent,
    pauschalierung = excluded.pauschalierung,
    pauschsteuersatz_bp = excluded.pauschsteuersatz_bp,
    soli_satz_bp = excluded.soli_satz_bp,
    gehaltsumwandlung = excluded.gehaltsumwandlung,
    bundesland = excluded.bundesland,
    kist_satz_bp = excluded.kist_satz_bp,
    eigenanteil_variante = excluded.eigenanteil_variante,
    monatslimit = excluded.monatslimit,
    limit_modus = excluded.limit_modus,
    eigene_feiertage = excluded.eigene_feiertage,
    notiz = excluded.notiz,
    geaendert_am = excluded.geaendert_am;

-- name: InsertBeleg :exec
INSERT INTO belege (
    id, datum, mahlzeit, bezugsort, arbeitsort, haendler_name, haendler_ort,
    belegbetrag_cent, korrigierter_betrag_cent, korrektur_grund, notiz, quelle,
    version, erstellt_am, geaendert_am, geloescht_am, loesch_grund
) VALUES (
    ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    1, ?, ?, NULL, NULL
);

-- name: UpdateBeleg :execresult
UPDATE belege
SET datum = ?,
    mahlzeit = ?,
    bezugsort = ?,
    arbeitsort = ?,
    haendler_name = ?,
    haendler_ort = ?,
    belegbetrag_cent = ?,
    korrigierter_betrag_cent = ?,
    korrektur_grund = ?,
    notiz = ?,
    quelle = ?,
    version = version + 1,
    geaendert_am = ?
WHERE id = ? AND version = ? AND geloescht_am IS NULL;

-- name: SoftDeleteBeleg :execresult
UPDATE belege
SET geloescht_am = ?,
    loesch_grund = ?,
    version = version + 1,
    geaendert_am = ?
WHERE id = ? AND version = ? AND geloescht_am IS NULL;

-- name: GetBeleg :one
SELECT * FROM belege WHERE id = ? AND geloescht_am IS NULL;

-- name: GetBelegByDatum :one
SELECT * FROM belege WHERE datum = ? AND geloescht_am IS NULL;

-- name: ListBelegeByMonat :many
SELECT * FROM belege
WHERE geloescht_am IS NULL AND datum >= ? AND datum < ?
ORDER BY datum ASC;

-- name: ListActiveBelege :many
SELECT * FROM belege
WHERE geloescht_am IS NULL
ORDER BY datum ASC, id ASC;

-- name: ListBelegbildKeys :many
SELECT blob_key, thumb_blob_key FROM belegbilder;

-- name: ListExportBlobKeys :many
SELECT pdf_blob_key, csv_blob_key, zip_blob_key FROM monatsexporte;

-- name: InsertBelegbild :exec
INSERT INTO belegbilder (
    id, beleg_id, seite, blob_key, thumb_blob_key, sha256, upload_sha256,
    mime, bytes, breite, hoehe, erkennung_status, erstellt_am
) VALUES (
    ?, NULL, NULL, ?, ?, ?, ?,
    'image/jpeg', ?, ?, ?, ?, ?
);

-- name: GetBelegbild :one
SELECT * FROM belegbilder WHERE id = ?;

-- name: ListBelegbilder :many
SELECT * FROM belegbilder ORDER BY id ASC;

-- name: UpdateBelegbildKeys :exec
UPDATE belegbilder SET blob_key = ?, thumb_blob_key = ? WHERE id = ?;

-- name: ListBelegbilderByBeleg :many
SELECT * FROM belegbilder WHERE beleg_id = ? ORDER BY seite ASC;

-- name: AssignBelegbild :exec
UPDATE belegbilder SET beleg_id = ?, seite = ? WHERE id = ?;

-- name: UnassignBelegbild :exec
UPDATE belegbilder SET beleg_id = NULL, seite = NULL WHERE id = ?;

-- name: DeleteUnassignedBelegbild :execresult
DELETE FROM belegbilder WHERE id = ? AND beleg_id IS NULL;

-- name: CountBlobKey :one
SELECT COUNT(*) FROM belegbilder WHERE blob_key = ? OR thumb_blob_key = ?;

-- FindDuplicateBild joins belege, so it cannot see an image that is still
-- waiting in *Offene Bilder*, which is the only kind of image the watched
-- folder creates. That intake needs exactly the question FindDuplicateBild
-- cannot answer: are these uploaded bytes already stored? A file whose store
-- succeeded but whose move into .processed failed stays in the folder and
-- would otherwise be stored again on every poll. upload_sha256 is the hash of
-- the uploaded bytes and is indexed, so this is a point lookup rather than a
-- table scan.
-- name: CountBelegbildByUploadSha :one
SELECT COUNT(*) FROM belegbilder WHERE upload_sha256 = ?;

-- name: CountExportBlobKey :one
SELECT COUNT(*) FROM monatsexporte
WHERE pdf_blob_key = sqlc.arg('key')
   OR csv_blob_key = sqlc.arg('key')
   OR zip_blob_key = sqlc.arg('key');

-- name: ListExportBelegIDs :many
SELECT beleg_ids FROM monatsexporte;

-- The expired unassigned images the sweeper removes, one batch at a time.
--
-- This replaces the unbounded read it grew out of: the same rows, a LIMIT, and
-- a cursor instead of an offset.
--
-- The cursor is the key of the last row of the previous batch, compared as the
-- pair (erstellt_am, id). An empty cursor means "start at the oldest"; the
-- first arm of the disjunction says so outright rather than leaning on both
-- columns being written by the application and never empty.
--
-- ORDER BY ascending reads the descending index belegbilder_offen_erstellt_am
-- backwards, so a batch stops after `limit` rows instead of walking the whole
-- backlog to filter and sort it. A keyset and not an offset because the caller
-- deletes what it reads: with OFFSET, deleting the rows of the previous batch
-- moves every row behind the window forward by exactly that many, so the next
-- batch steps over live rows and leaves them expired for another hour.
-- name: ListUnassignedBelegbilderBeforePage :many
SELECT * FROM belegbilder
WHERE beleg_id IS NULL
  AND erstellt_am < sqlc.arg('before')
  AND (
    sqlc.arg('after') = ''
    OR erstellt_am > sqlc.arg('after')
    OR (erstellt_am = sqlc.arg('after') AND id > sqlc.arg('after_id'))
  )
ORDER BY erstellt_am, id
LIMIT sqlc.arg('limit');

-- The "offene Bilder" list: uploaded images that no Beleg owns yet, newest
-- first. An empty status means "no filter"; the paged count runs the same
-- predicate so the total and the page always agree.
-- name: ListUnassignedBilder :many
SELECT id, erstellt_am, erkennung_status, erkennung_fehler, breite, hoehe
FROM belegbilder
WHERE beleg_id IS NULL
  AND (sqlc.arg('status') = '' OR erkennung_status = sqlc.arg('status'))
ORDER BY erstellt_am DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountUnassignedBilder :one
SELECT COUNT(*) FROM belegbilder
WHERE beleg_id IS NULL
  AND (sqlc.arg('status') = '' OR erkennung_status = sqlc.arg('status'));

-- name: FindDuplicateBild :one
SELECT b.id, b.datum
FROM belegbilder i
JOIN belege b ON b.id = i.beleg_id
WHERE b.geloescht_am IS NULL
  AND (sqlc.arg('exclude_id') = '' OR b.id != sqlc.arg('exclude_id'))
  AND (i.sha256 = sqlc.arg('sha256') OR i.upload_sha256 = sqlc.arg('upload_sha256'))
LIMIT 1;

-- FindDuplicateBilder for a whole page of images at once.
--
-- The predicate is the union of the per-image one: a hash comes back exactly
-- when it would have come back for at least one of the images on the page,
-- because `sha256 IN (...) OR upload_sha256 IN (...)` holds for a row if and
-- only if it holds for one of the individual pairs. Which of the page's images
-- a hash belongs to is left to the caller, which holds the pairs and would
-- otherwise have to run one query per image.
--
-- One row per hash, never one per image that carries it. The caller asks
-- whether SOME other receipt holds the picture, so the second and the
-- thousandth receipt to turn up with the same hash add nothing but work. The
-- picture a folder watcher uploaded twice is a hash N receipts share, which is
-- exactly the case that turned this page's read into one row per receipt.
--
-- The GROUP BY over the matched column is what collapses them, and
-- CAST(MIN(b.id) AS TEXT) names the lowest of the receipts sharing the hash.
-- That is the same row the old ORDER BY b.id, i.id put first, and a reproducible
-- answer matters: the per-image lookup takes an arbitrary one of several
-- candidates (LIMIT 1 with no ordering) and a caller building a map has to be
-- able to say which one it picked. The cast is only there to keep the column
-- TEXT for the generated code; MIN over a TEXT column already is one.
--
-- The two columns the per-image lookup ORs together are two halves here rather
-- than one predicate, because a row can match on either and GROUP BY can only
-- pick one of them. Both columns are indexed and each half is an IN lookup on
-- its own column, so this stays two index lookups rather than a table scan.
--
-- That split costs something the single predicate did not: a hash is only
-- comparable to the column it was matched in. The page's sha256s and its
-- upload_sha256s overlap all the time: sha256 is the digest of the normalised
-- JPEG, upload_sha256 the digest of the file that was uploaded, and a hash one
-- of a receipt's images carries in one column is routinely what another of its
-- images carries in the other. So each half says which column it matched in
-- (quelle: 0 for sha256, 1 for upload_sha256) and the caller keys its map on
-- that as well as on the hash. A caller that merged the two halves would name a
-- receipt as the duplicate of an image the per-image lookup does not match.
-- name: FindDuplicateBilder :many
SELECT i.sha256 AS hash, CAST(0 AS INTEGER) AS quelle, CAST(MIN(b.id) AS TEXT) AS id
FROM belegbilder i
JOIN belege b ON b.id = i.beleg_id
WHERE b.geloescht_am IS NULL
  AND (sqlc.arg('exclude_id') = '' OR b.id != sqlc.arg('exclude_id'))
  AND i.sha256 IN (sqlc.slice('sha256s'))
GROUP BY i.sha256
UNION ALL
SELECT i.upload_sha256 AS hash, CAST(1 AS INTEGER) AS quelle, CAST(MIN(b.id) AS TEXT) AS id
FROM belegbilder i
JOIN belege b ON b.id = i.beleg_id
WHERE b.geloescht_am IS NULL
  AND (sqlc.arg('exclude_id') = '' OR b.id != sqlc.arg('exclude_id'))
  AND i.upload_sha256 IN (sqlc.slice('upload_sha256s'))
GROUP BY i.upload_sha256;

-- name: ListErkennungen :many
SELECT b.id, b.datum, b.haendler_name, b.belegbetrag_cent, i.erkennung_ergebnis
FROM belege b
JOIN belegbilder i ON i.beleg_id = b.id
WHERE b.geloescht_am IS NULL
  AND i.erkennung_ergebnis IS NOT NULL
  AND (sqlc.arg('exclude_id') = '' OR b.id != sqlc.arg('exclude_id'));

-- name: GetMonat :one
SELECT monat, status, gesperrt_am, letzte_exportversion FROM monate WHERE monat = ?;

-- name: UpsertMonatStatus :exec
INSERT INTO monate (monat, status, gesperrt_am, letzte_exportversion)
VALUES (?, ?, ?, ?)
ON CONFLICT (monat) DO UPDATE SET
    status = excluded.status,
    gesperrt_am = COALESCE(monate.gesperrt_am, excluded.gesperrt_am);

-- name: MarkGesperrtGeaendert :exec
UPDATE monate
SET status = 'geaendert'
WHERE monat >= ? AND monat <= ? AND status = 'gesperrt';

-- name: CountLockedMonths :one
SELECT COUNT(*) FROM monate
WHERE monat >= ? AND monat <= ? AND status IN ('gesperrt', 'geaendert');

-- name: ListExporte :many
SELECT id, monat, version, erstellt_am, csv_blob_key, zip_blob_key
FROM monatsexporte
WHERE monat = ?
ORDER BY version ASC;

-- name: LatestExportAm :one
SELECT erstellt_am, version FROM monatsexporte
WHERE monat = ?
ORDER BY version DESC
LIMIT 1;

-- name: LatestExport :one
SELECT id, version, protokoll_hash, erstellt_am
FROM monatsexporte
WHERE monat = ?
ORDER BY version DESC
LIMIT 1;

-- name: GetMonatsexport :one
SELECT * FROM monatsexporte WHERE id = ?;

-- name: InsertMonatsexport :exec
INSERT INTO monatsexporte (
    id, monat, version, erstellt_am,
    pdf_blob_key, pdf_sha256,
    csv_blob_key, csv_sha256,
    zip_blob_key, zip_sha256,
    regeln_snapshot, einstellungen_snapshot, summen, beleg_ids,
    erklaerung_bestaetigt_am, warnungen_bestaetigt, protokoll_hash
) VALUES (
    ?, ?, ?, ?,
    ?, ?,
    ?, ?,
    ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?
);

-- name: LockMonat :exec
INSERT INTO monate (monat, status, gesperrt_am, letzte_exportversion)
VALUES (?, 'gesperrt', ?, ?)
ON CONFLICT (monat) DO UPDATE SET
    status = 'gesperrt',
    gesperrt_am = COALESCE(monate.gesperrt_am, excluded.gesperrt_am),
    letzte_exportversion = excluded.letzte_exportversion;

-- name: InsertSitzung :exec
INSERT INTO sitzungen (
    token_hash, akteur, erstellt_am, zuletzt_aktiv_am, laeuft_ab_am, user_agent, ip, oidc_id_token
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSitzung :one
SELECT token_hash, akteur, erstellt_am, zuletzt_aktiv_am, laeuft_ab_am, user_agent, ip, oidc_id_token
FROM sitzungen
WHERE token_hash = ?;

-- name: TouchSitzung :exec
UPDATE sitzungen SET zuletzt_aktiv_am = ? WHERE token_hash = ?;

-- name: DeleteSitzung :exec
DELETE FROM sitzungen WHERE token_hash = ?;

-- name: DeleteOtherSitzungen :exec
DELETE FROM sitzungen WHERE token_hash != ?;

-- name: DeleteAllSitzungen :exec
DELETE FROM sitzungen;

-- name: DeleteExpiredSitzungen :exec
DELETE FROM sitzungen WHERE laeuft_ab_am <= ? OR zuletzt_aktiv_am <= ?;

-- name: ListSitzungen :many
SELECT token_hash, akteur, erstellt_am, zuletzt_aktiv_am, laeuft_ab_am, user_agent, ip, oidc_id_token
FROM sitzungen
ORDER BY zuletzt_aktiv_am DESC;

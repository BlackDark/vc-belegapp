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

-- name: CountExportBlobKey :one
SELECT COUNT(*) FROM monatsexporte
WHERE pdf_blob_key = sqlc.arg('key')
   OR csv_blob_key = sqlc.arg('key')
   OR zip_blob_key = sqlc.arg('key');

-- name: ListExportBelegIDs :many
SELECT beleg_ids FROM monatsexporte;

-- name: ListUnassignedBelegbilderBefore :many
SELECT * FROM belegbilder WHERE beleg_id IS NULL AND erstellt_am < ?;

-- name: FindDuplicateBild :one
SELECT b.id, b.datum
FROM belegbilder i
JOIN belege b ON b.id = i.beleg_id
WHERE b.geloescht_am IS NULL
  AND (sqlc.arg('exclude_id') = '' OR b.id != sqlc.arg('exclude_id'))
  AND (i.sha256 = sqlc.arg('sha256') OR i.upload_sha256 = sqlc.arg('upload_sha256'))
LIMIT 1;

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

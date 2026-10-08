-- +goose Up
CREATE TABLE einstellungen (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    arbeitnehmer_name TEXT NOT NULL DEFAULT '',
    personalnummer TEXT NOT NULL DEFAULT '',
    arbeitgeber_name TEXT NOT NULL DEFAULT '',
    standard_bezugsort TEXT NOT NULL DEFAULT 'supermarkt' CHECK (
        standard_bezugsort IN ('supermarkt', 'restaurant', 'kantine', 'baeckerei', 'lieferdienst', 'sonstiges')
    ),
    standard_arbeitsort TEXT NOT NULL DEFAULT 'betrieb' CHECK (standard_arbeitsort IN ('betrieb', 'homeoffice')),
    erkennung_aktiv INTEGER NOT NULL DEFAULT 1 CHECK (erkennung_aktiv IN (0, 1)),
    export_zip_standard INTEGER NOT NULL DEFAULT 0 CHECK (export_zip_standard IN (0, 1)),
    export_csv_standard INTEGER NOT NULL DEFAULT 1 CHECK (export_csv_standard IN (0, 1)),
    geaendert_am TEXT NOT NULL
);

INSERT INTO einstellungen (id, geaendert_am) VALUES (1, '1970-01-01T00:00:00Z');

CREATE TABLE jahresregeln (
    jahr INTEGER PRIMARY KEY CHECK (jahr BETWEEN 2020 AND 2100),
    zuschuss_cent INTEGER NOT NULL CHECK (zuschuss_cent > 0),
    mahlzeiten TEXT NOT NULL,
    standard_mahlzeit TEXT NOT NULL DEFAULT 'mittag' CHECK (standard_mahlzeit IN ('fruehstueck', 'mittag', 'abend')),
    sbw_fruehstueck_cent INTEGER NOT NULL CHECK (sbw_fruehstueck_cent > 0),
    sbw_mittag_cent INTEGER NOT NULL CHECK (sbw_mittag_cent > 0),
    sbw_abend_cent INTEGER NOT NULL CHECK (sbw_abend_cent > 0),
    hoechstzuschuss_aufschlag_cent INTEGER NOT NULL DEFAULT 310,
    pauschalierung INTEGER NOT NULL DEFAULT 1 CHECK (pauschalierung IN (0, 1)),
    pauschsteuersatz_bp INTEGER NOT NULL DEFAULT 2500,
    soli_satz_bp INTEGER NOT NULL DEFAULT 550,
    gehaltsumwandlung INTEGER NOT NULL DEFAULT 0 CHECK (gehaltsumwandlung IN (0, 1)),
    bundesland TEXT NOT NULL CHECK (
        bundesland IN ('BW', 'BY', 'BE', 'BB', 'HB', 'HH', 'HE', 'MV', 'NI', 'NW', 'RP', 'SL', 'SN', 'ST', 'SH', 'TH')
    ),
    kist_satz_bp INTEGER NOT NULL CHECK (kist_satz_bp BETWEEN 0 AND 900),
    eigenanteil_variante TEXT NOT NULL DEFAULT 'standard' CHECK (eigenanteil_variante IN ('standard', 'vorsichtig')),
    monatslimit INTEGER NOT NULL DEFAULT 15 CHECK (monatslimit BETWEEN 1 AND 31),
    limit_modus TEXT NOT NULL DEFAULT 'warnen' CHECK (limit_modus IN ('warnen', 'blockieren')),
    eigene_feiertage TEXT NOT NULL DEFAULT '[]',
    notiz TEXT NOT NULL DEFAULT '',
    erstellt_am TEXT NOT NULL,
    geaendert_am TEXT NOT NULL
);

CREATE TABLE belege (
    id TEXT PRIMARY KEY,
    datum TEXT NOT NULL,
    mahlzeit TEXT NOT NULL CHECK (mahlzeit IN ('fruehstueck', 'mittag', 'abend')),
    bezugsort TEXT NOT NULL CHECK (
        bezugsort IN ('supermarkt', 'restaurant', 'kantine', 'baeckerei', 'lieferdienst', 'sonstiges')
    ),
    arbeitsort TEXT NOT NULL CHECK (arbeitsort IN ('betrieb', 'homeoffice')),
    haendler_name TEXT NOT NULL CHECK (length(haendler_name) BETWEEN 1 AND 120),
    haendler_ort TEXT NOT NULL DEFAULT '' CHECK (length(haendler_ort) <= 120),
    belegbetrag_cent INTEGER NOT NULL CHECK (belegbetrag_cent BETWEEN 1 AND 100000),
    korrigierter_betrag_cent INTEGER,
    korrektur_grund TEXT,
    notiz TEXT NOT NULL DEFAULT '' CHECK (length(notiz) <= 500),
    quelle TEXT NOT NULL CHECK (quelle IN ('manuell', 'ki', 'ki_korrigiert')),
    version INTEGER NOT NULL DEFAULT 1,
    erstellt_am TEXT NOT NULL,
    geaendert_am TEXT NOT NULL,
    geloescht_am TEXT,
    loesch_grund TEXT,
    CHECK (
        korrigierter_betrag_cent IS NULL
        OR (korrigierter_betrag_cent >= 0 AND korrigierter_betrag_cent <= belegbetrag_cent)
    ),
    CHECK (
        (
            korrigierter_betrag_cent IS NULL
            AND korrektur_grund IS NULL
        )
        OR (
            korrigierter_betrag_cent IS NOT NULL
            AND korrektur_grund IS NOT NULL
            AND length(korrektur_grund) >= 3
        )
    )
);

CREATE UNIQUE INDEX belege_datum_aktiv ON belege (datum) WHERE geloescht_am IS NULL;

CREATE TABLE belegbilder (
    id TEXT PRIMARY KEY,
    beleg_id TEXT NULL REFERENCES belege (id),
    seite INTEGER NULL CHECK (seite IS NULL OR seite BETWEEN 1 AND 3),
    blob_key TEXT NOT NULL,
    thumb_blob_key TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    upload_sha256 TEXT NOT NULL,
    mime TEXT NOT NULL CHECK (mime = 'image/jpeg'),
    bytes INTEGER NOT NULL,
    breite INTEGER NOT NULL,
    hoehe INTEGER NOT NULL,
    erkennung_status TEXT NOT NULL CHECK (erkennung_status IN ('keine', 'ausstehend', 'laeuft', 'fertig', 'fehler')),
    erkennung_ergebnis TEXT NULL,
    erkennung_roh TEXT NULL,
    erkennung_fehler TEXT NULL,
    erkennung_modell TEXT NULL,
    erkennung_dauer_ms INTEGER NULL,
    erstellt_am TEXT NOT NULL,
    CHECK (
        (beleg_id IS NULL AND seite IS NULL)
        OR (beleg_id IS NOT NULL AND seite IS NOT NULL)
    )
);

CREATE UNIQUE INDEX belegbilder_beleg_seite ON belegbilder (beleg_id, seite);
CREATE INDEX belegbilder_sha256 ON belegbilder (sha256);
CREATE INDEX belegbilder_upload_sha256 ON belegbilder (upload_sha256);
CREATE INDEX belegbilder_beleg ON belegbilder (beleg_id);

CREATE TABLE monate (
    monat TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'offen' CHECK (status IN ('offen', 'gesperrt', 'geaendert')),
    gesperrt_am TEXT NULL,
    letzte_exportversion INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE monatsexporte (
    id TEXT PRIMARY KEY,
    monat TEXT NOT NULL REFERENCES monate (monat),
    version INTEGER NOT NULL CHECK (version >= 1),
    erstellt_am TEXT NOT NULL,
    pdf_blob_key TEXT NOT NULL,
    pdf_sha256 TEXT NOT NULL,
    csv_blob_key TEXT NULL,
    csv_sha256 TEXT NULL,
    zip_blob_key TEXT NULL,
    zip_sha256 TEXT NULL,
    regeln_snapshot TEXT NOT NULL,
    einstellungen_snapshot TEXT NOT NULL,
    summen TEXT NOT NULL,
    beleg_ids TEXT NOT NULL,
    erklaerung_bestaetigt_am TEXT NOT NULL,
    warnungen_bestaetigt INTEGER NOT NULL,
    protokoll_hash TEXT NOT NULL,
    UNIQUE (monat, version)
);

CREATE TABLE aenderungsprotokoll (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    zeitpunkt TEXT NOT NULL,
    akteur TEXT NOT NULL,
    aktion TEXT NOT NULL,
    entitaet TEXT NOT NULL,
    entitaet_id TEXT NOT NULL,
    monat TEXT NULL,
    vorher TEXT NULL,
    nachher TEXT NULL,
    diff TEXT NULL,
    grund TEXT NULL,
    request_id TEXT NOT NULL,
    prev_hash TEXT NOT NULL,
    hash TEXT NOT NULL
);

CREATE INDEX aenderungsprotokoll_monat ON aenderungsprotokoll (monat);
CREATE INDEX aenderungsprotokoll_entitaet ON aenderungsprotokoll (entitaet_id);

-- +goose StatementBegin
CREATE TRIGGER aenderungsprotokoll_no_update
BEFORE UPDATE ON aenderungsprotokoll
BEGIN
    SELECT RAISE(ABORT, 'append-only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER aenderungsprotokoll_no_delete
BEFORE DELETE ON aenderungsprotokoll
BEGIN
    SELECT RAISE(ABORT, 'append-only');
END;
-- +goose StatementEnd

CREATE TABLE sitzungen (
    token_hash TEXT PRIMARY KEY,
    akteur TEXT NOT NULL,
    erstellt_am TEXT NOT NULL,
    zuletzt_aktiv_am TEXT NOT NULL,
    laeuft_ab_am TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    ip TEXT NOT NULL,
    oidc_id_token TEXT NOT NULL DEFAULT ''
);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    typ TEXT NOT NULL CHECK (typ IN ('erkennung', 'datenexport')),
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('wartend', 'laeuft', 'fertig', 'fehler')),
    versuche INTEGER NOT NULL DEFAULT 0,
    naechster_versuch_am TEXT NOT NULL,
    ergebnis TEXT NULL,
    fehler TEXT NULL,
    erstellt_am TEXT NOT NULL,
    geaendert_am TEXT NOT NULL
);

-- +goose Down
DROP TABLE jobs;
DROP TABLE sitzungen;
DROP TRIGGER aenderungsprotokoll_no_delete;
DROP TRIGGER aenderungsprotokoll_no_update;
DROP TABLE aenderungsprotokoll;
DROP TABLE monatsexporte;
DROP TABLE monate;
DROP TABLE belegbilder;
DROP TABLE belege;
DROP TABLE jahresregeln;
DROP TABLE einstellungen;

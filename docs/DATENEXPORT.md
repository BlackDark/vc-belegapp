# Datenexport

A Datenexport is a zip archive of one Belegapp instance: the SQLite database, every referenced blob, and a CSV of active Belege. Format version 1. Import replaces the whole dataset. It does not merge.

## Produce and restore

```bash
belegapp backup --out /data/backups/belegapp.zip
belegapp restore /data/backups/belegapp.zip --yes
```

`restore` refuses to run without `--yes`. Both commands use the same environment as `serve` (`BELEGAPP_DATA_DIR`, storage backend, database path).

HTTP, under `/api/v1`, session required:

| Method | Path | Result |
|---|---|---|
| POST | `/datenexport` | `202 {"job_id"}` |
| GET | `/jobs/{id}` | `ergebnis.download_url` when `status` is `fertig` |
| GET | `/datenexport/{job_id}/datei` | zip attachment, valid for 24 hours |
| POST | `/datenimport/pruefen` | multipart field `datei`; summary and `import_token` (30 minutes) |
| POST | `/datenimport` | JSON `import_token` and `bestaetigung: "ERSETZEN"` |

The UI lives on Einstellungen. The confirmation word is exactly `ERSETZEN`. After a successful import every Sitzung ends.

A non-empty instance is replaced. Before the replacement the server writes `datenexporte/vor-import-<timestamp>.zip` into the blob store and keeps a local copy for rollback. User download zips under `datenexporte/` are deleted after 24 hours. The safety zip is kept.

## Archive layout

```
manifest.json
db/belegapp.sqlite
csv/belege.csv
blobs/<storage key>
```

`manifest.json`:

```json
{
  "format": "vc-belegapp-datenexport",
  "format_version": 1,
  "app_version": "1.0.0",
  "schema_version": 3,
  "erstellt_am": "2026-10-09T12:00:00Z",
  "instanz_id": "<id>",
  "zeitraum": { "von": "2024-03-04", "bis": "2024-03-04" },
  "anzahl_belege": 1,
  "dateien": [{ "pfad": "db/belegapp.sqlite", "sha256": "<hex>", "bytes": 0 }]
}
```

`schema_version` is the highest applied goose migration. `zeitraum` is the inclusive span of active Beleg dates, or empty strings when there are none. `dateien` lists every zip member except `manifest.json`, with SHA-256 and size. Paths use `/`, stay inside the archive, and match `[a-z0-9/._-]`.

The database member is a `VACUUM INTO` snapshot. Sitzungen and jobs are removed from that copy. The live database is not modified by the snapshot. The audit row `datenexport_erstellt` is written after the snapshot, so it is not inside the archive. The audit row `datenimport` is written after restore; its entity id is the SHA-256 of the raw `manifest.json` bytes.

Blobs are the keys referenced by Belegbilder (image and thumbnail) and Monatsexporte (PDF, CSV, ZIP). Previous Datenexport zips are not included. On import, a destination key with the same SHA-256 is left as it is. A different hash is overwritten. Keys that exist only on the destination are left in place.

A soft-deleted Beleg that is not listed in any Monatsexport has its images unassigned. After `BELEGAPP_UNASSIGNED_IMAGE_TTL` those rows and blobs are gone, so a later archive does not contain them. The Beleg row and the audit log stay. Images of a Beleg that appears in `monatsexporte.beleg_ids` stay assigned, and Monatsexport blobs are never deleted.

`csv/belege.csv` is UTF-8 with BOM, `;` separated, CRLF. The monthly header plus `beleg_id`, all years, active Belege only, and a `summe` line.

## Rejected archives

Import checks the layout, `format` / `format_version`, every checksum and size, then the audit hash chain. Goose migrations run after the database file is replaced. A failure rolls back the database and the blobs copied from the safety export.

| Condition | Status | Code |
|---|---|---|
| Not a zip, bad manifest, checksum mismatch, broken hash chain, confirmation is not `ERSETZEN` | 422 | `E_IMPORT_UNGUELTIG` |
| `schema_version` newer than this binary | 422 | `E_IMPORT_ZU_NEU` |
| File larger than `BELEGAPP_IMPORT_MAX_BYTES` | 413 | `E_IMPORT_UNGUELTIG` |
| Another import is in progress | 503 | `E_IMPORT_LAEUFT` |

An unknown `format_version` is `E_IMPORT_UNGUELTIG`. A schema older than the binary is migrated with goose.

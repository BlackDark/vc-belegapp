# Backup and restore

A Datenexport is a zip of the whole instance (SQLite snapshot, referenced blobs, CSV). It is separate from the Monatsexport. Import replaces the dataset; it does not merge. The server writes a safety copy first. Layout, rejection codes, and retention of unassigned images: [DATENEXPORT.md](DATENEXPORT.md).

In the app: Einstellungen → Datenexport, then Datenimport. The confirmation word is `ERSETZEN`. Every session ends after a successful import.

```bash
belegapp backup --out /data/backups/belegapp.zip
belegapp restore /data/backups/belegapp.zip --yes
```

`restore` refuses to run without `--yes`. Both commands use the same environment as `serve` (`BELEGAPP_DATA_DIR`, database path, storage backend).

The Kubernetes CronJob [deploy/k8s/cronjob.yaml](../deploy/k8s/cronjob.yaml) runs `belegapp backup --out /data/backups/belegapp.zip` at 03:15. A ReadWriteOnce volume often cannot be mounted by that job while the Deployment holds it. See [Kubernetes](deployment.md#kubernetes). A volume snapshot is a useful second copy; the zip is the format that round-trips blobs and the audit chain.

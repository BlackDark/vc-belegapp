# 0004 Bildablage hinter BlobStore-Interface (Dateisystem und S3)
Status: angenommen (2026-10-08, Interview Runde 3)
Kontext: Betrieb unter Docker und Kubernetes; Bilder sollen wahlweise lokal oder in S3 liegen.
Entscheidung: Interface `BlobStore` mit Implementierungen `fs` (os.Root, atomares Schreiben) und `s3` (minio-go v7), beide ab v1; inhaltsadressierte Keys (SHA-256). Backend-Wechsel über Datenexport/Datenimport.
Konsequenzen: Contract-Tests gegen beide Backends; SQLite bleibt immer auf dem Volume (ein Replica, kein NFS). Details: SPEC.md §11.

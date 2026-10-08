# 0001 Go-Single-Binary mit SQLite und eingebettetem SolidJS-Frontend
Status: angenommen (2026-10-08)
Kontext: Self-hosted, Single-User, Docker/K8s, non-root, leichtgewichtig.
Entscheidung: Go (chi, modernc sqlite, sqlc, goose), Frontend SolidJS + Kobalte/solid-ui + Tailwind 4 eingebettet; Bilder hinter Storage-Interface (Dateisystem/S3).
Konsequenzen: Eine Instanz (SQLite), kein NFS-Volume; Bun/Hono, Rust, PocketBase verworfen (siehe research/stack.md).

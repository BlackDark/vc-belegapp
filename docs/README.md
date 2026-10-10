# Documentation

Start with the [project README](../README.md) for screenshots and the quickstart. The application UI is German; these docs are English with German domain terms (see the [glossary](GLOSSARY.md)).

## Using

- [Calculation](calculation.md) — how Zuschuss, Sachbezug, Pauschalsteuer, Soli, and Kirchensteuer are computed, plus the disclaimer.
- [Features](FEATURES.md) — everything that is implemented, with the PR and release that introduced it.

## Operating

- [Configuration](configuration.md) — every `BELEGAPP_` variable, including `_FILE` secrets.
- [Deployment](deployment.md) — Docker, Compose, Kubernetes/Kustomize, binary, reverse proxy, OIDC.
- [Backup](backup.md) — Datenexport, scheduled backups, and restore.
- [Datenexport format](DATENEXPORT.md) — zip layout, manifest, and import rejection codes.

## Reference & spec

- [Specification](SPEC.md) — the product specification (German), including §19.4 intentional deviations.
- [Glossary](GLOSSARY.md) — German domain terms.
- [Tax research](research/steuer.md) — the tax rules behind the calculation (German, no tax advice).
- [Stack research](research/stack.md) — why this tech stack (German).

## Decisions

- [0001](adr/0001-go-single-binary-sqlite.md) — one Go binary, SQLite, embedded SolidJS.
- [0002](adr/0002-typst-pdf.md) — Monatsexport via the Typst CLI, PDF/A-2b.
- [0003](adr/0003-auth-passwort-und-oidc.md) — password and OIDC together.
- [0004](adr/0004-storage-fs-und-s3.md) — blobs on disk or S3, content-addressed.
- [0005](adr/0005-monatssperre-aenderungsprotokoll.md) — month lock and hash-chained audit log.
- [0006](adr/0006-belegerkennung-openai-kompatibel.md) — recognition over OpenAI-compatible chat completions.
- [Interview notes](NOTES.md) — the original requirement decisions (2026-10-08).

## Project

- [Roadmap](ROADMAP.md) — approved backlog, rejected ideas, and possible features.
- [Development](development.md) — architecture, make targets, tests, screenshots.
- [Releases](releases.md) — release-please, GoReleaser, and how to verify signatures.
- [Changelog](../CHANGELOG.md) — release history.

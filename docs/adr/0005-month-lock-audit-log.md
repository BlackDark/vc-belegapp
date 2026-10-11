# 0005 Monatssperre nach Export und Änderungsprotokoll mit Hash-Kette
Status: angenommen (2026-10-08, Interview Runde 3); Ausgestaltung (kein Entsperren, Exportversionen) vorgeschlagen
Kontext: Monatsexporte sollen prüfungssicher sein; Korrekturen müssen nachvollziehbar bleiben.
Entscheidung: Finaler Monatsexport setzt Monatsstatus `gesperrt`; Änderungen danach nur mit Änderungsgrund, Status `geaendert`, neuer Export als Exportversion n+1 mit Änderungsliste. Alle fachlichen Änderungen landen append-only (SQLite-Trigger) im Änderungsprotokoll mit SHA-256-Hash-Kette; exportierte PDFs werden nie gelöscht.
Konsequenzen: Soft-Delete für Belege; Prüfpunkt „Protokoll intakt“ vor jedem Export. Details: SPEC.md §5.2, §7.3, §12.

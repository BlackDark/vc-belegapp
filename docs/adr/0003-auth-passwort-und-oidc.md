# 0003 Login per Passwort und OIDC
Status: angenommen (2026-10-08, Interview Runde 3)
Kontext: Single-User; Eduard nutzt primär seinen OIDC-Provider, Passwort als Fallback.
Entscheidung: Beide Methoden ab v1. Passwort als argon2id-Hash per Env/Secret; OIDC Authorization Code + PKCE (go-oidc), Allowlist für `sub` oder verifizierte E-Mail; serverseitige Sitzungen in SQLite, Cookie `__Host-…`, CSRF über `http.CrossOriginProtection` + SameSite=Lax.
Konsequenzen: Keine Benutzerverwaltung/kein Passwortwechsel in der UI; Start schlägt fehl ohne konfigurierte Methode bzw. ohne OIDC-Allowlist. Details: SPEC.md §9.

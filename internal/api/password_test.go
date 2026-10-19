package api

import (
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// A password change from the settings screen: the current password is required,
// the new one is what logs in afterwards, and the sessions opened with the old
// one are gone. The cookie that made the change stays valid.
func TestChangePasswordFlow(t *testing.T) {
	loc := dbtest.Loc(t)
	database := dbtest.Open(t)
	store := storage.NewFS(t.TempDir())
	svc := newTestService(t, database, store, dbtest.Clock(loc), nil)
	ts, client := newTestServer(t, newTestAPI(t, svc, store))
	login(t, ts.URL, client)

	// A second browser signed in with the old password.
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar}
	login(t, ts.URL, other)

	const neues = "ein-anderes-langes-passwort"
	res := doJSON(t, client, http.MethodPut, ts.URL+"/api/v1/auth/passwort", "",
		map[string]string{"aktuell": "falsch", "neu": neues})
	if res.Status != 422 || res.JSON["code"] != "E_PASSWORT_FALSCH" {
		t.Fatalf("wrong current password: %d %s", res.Status, res.Raw)
	}
	res = doJSON(t, client, http.MethodPut, ts.URL+"/api/v1/auth/passwort", "",
		map[string]string{"aktuell": "geheim", "neu": "kurz"})
	if res.Status != 422 || res.JSON["code"] != "E_PASSWORT_ZU_KURZ" {
		t.Fatalf("short new password: %d %s", res.Status, res.Raw)
	}

	res = doJSON(t, client, http.MethodPut, ts.URL+"/api/v1/auth/passwort", "",
		map[string]string{"aktuell": "geheim", "neu": neues})
	if res.Status != 204 {
		t.Fatalf("change: %d %s", res.Status, res.Raw)
	}

	res = doJSON(t, client, http.MethodGet, ts.URL+"/api/v1/auth/me", "", nil)
	if res.Status != 200 {
		t.Fatalf("the session that changed the password was dropped: %d", res.Status)
	}
	res = doJSON(t, other, http.MethodGet, ts.URL+"/api/v1/auth/me", "", nil)
	if res.Status != 401 {
		t.Fatalf("the other session survived the change: %d", res.Status)
	}

	res = doJSON(t, other, http.MethodPost, ts.URL+"/api/v1/auth/login", "", map[string]string{"passwort": "geheim"})
	if res.Status != 401 {
		t.Fatalf("the old password still logs in: %d", res.Status)
	}
	res = doJSON(t, other, http.MethodPost, ts.URL+"/api/v1/auth/login", "", map[string]string{"passwort": neues})
	if res.Status != 204 {
		t.Fatalf("the new password does not log in: %d %s", res.Status, res.Raw)
	}
}

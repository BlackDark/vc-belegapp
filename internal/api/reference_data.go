package api

import (
	"net/http"

	"github.com/BlackDark/vc-belegapp/internal/rules"
)

// referenceDataResponse is the body of GET /werte. Every list is explicitly
// ordered; no Go map ever reaches the encoder, so the order is stable for the
// client.
type referenceDataResponse struct {
	Sachbezugswerte []rules.SBWEntry   `json:"sachbezugswerte"`
	Kirchensteuer   []rules.KistSatzBP `json:"kirchensteuer"`
	Bundeslaender   []string           `json:"bundeslaender"`
	Mahlzeiten      []rules.Label      `json:"mahlzeiten"`
	Bezugsorte      []rules.Label      `json:"bezugsorte"`
}

func (a *API) referenceData(w http.ResponseWriter, _ *http.Request) {
	a.writeJSON(w, http.StatusOK, referenceDataResponse{
		Sachbezugswerte: rules.SBWListe(),
		Kirchensteuer:   rules.KistSaetze(),
		Bundeslaender:   rules.Bundeslaender(),
		Mahlzeiten:      rules.Mahlzeiten.Entries(),
		Bezugsorte:      rules.Bezugsorte.Entries(),
	})
}

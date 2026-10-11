package api

import (
	"net/http"

	"github.com/BlackDark/vc-belegapp/internal/rules"
)

// werteResponse is the body of GET /werte. Every list is explicitly ordered;
// no Go map ever reaches the encoder, so the order is stable for the client.
type werteResponse struct {
	Sachbezugswerte []rules.SBWEntry   `json:"sachbezugswerte"`
	Kirchensteuer   []rules.KistSatzBP `json:"kirchensteuer"`
	Bundeslaender   []string           `json:"bundeslaender"`
	Mahlzeiten      []rules.Label      `json:"mahlzeiten"`
	Bezugsorte      []rules.Label      `json:"bezugsorte"`
}

func (a *API) werte(w http.ResponseWriter, _ *http.Request) {
	a.writeJSON(w, http.StatusOK, werteResponse{
		Sachbezugswerte: rules.SBWListe(),
		Kirchensteuer:   rules.KistSaetze(),
		Bundeslaender:   rules.Bundeslaender(),
		Mahlzeiten:      rules.Mahlzeiten.Entries(),
		Bezugsorte:      rules.Bezugsorte.Entries(),
	})
}

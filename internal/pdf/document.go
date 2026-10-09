package pdf

// Erklaerung is the fixed employee declaration from the specification.
const Erklaerung = "Ich versichere, dass jeder aufgeführte Beleg eine Mahlzeit betrifft, die ich an dem angegebenen Tag als Arbeitstag (kein Urlaub, keine Krankheit, keine Auswärtstätigkeit) selbst erworben habe und die zum Verzehr an diesem Tag bestimmt war. Nicht erstattungsfähige Artikel (z. B. Alkohol, Tabak, Pfand, Non-Food, Vorratskäufe) habe ich herausgerechnet. Jeder Beleg wird nur einmal eingereicht."

// HinweisLohn is printed under the declaration.
const HinweisLohn = "Vorberechnung nach R 8.1 Abs. 7 Nr. 4 LStR / § 40 Abs. 2 Satz 1 Nr. 1 EStG; maßgeblich ist die Lohnabrechnung des Arbeitgebers."

// Document is the JSON document the Typst template reads.
type Document struct {
	Meta          Meta          `json:"meta"`
	Stamm         Stamm         `json:"stamm"`
	Regel         []Paar        `json:"regel"`
	RegelHinweise []string      `json:"regel_hinweise"`
	Belege        []Zeile       `json:"belege"`
	Summen        SummenBlock   `json:"summen"`
	Pruefpunkte   []Check       `json:"pruefpunkte"`
	Fussnoten     []string      `json:"fussnoten"`
	Aenderungen   []Aenderung   `json:"aenderungen"`
	Erklaerung    string        `json:"erklaerung"`
	Bestaetigung  string        `json:"bestaetigung"`
	Hinweis       string        `json:"hinweis"`
	Anhang        []AnhangSeite `json:"anhang"`
	ErstelltUnix  int64         `json:"-"`
	Entwurf       bool          `json:"-"`
}

// Meta is the document header.
type Meta struct {
	Titel      string `json:"titel"`
	PDFTitel   string `json:"pdf_titel"`
	Autor      string `json:"autor"`
	DokumentID string `json:"dokument_id"`
	Erstellt   string `json:"erstellt"`
	AppVersion string `json:"app_version"`
	Version    int    `json:"version"`
}

// Stamm is the employee and employer block.
type Stamm struct {
	Arbeitnehmer   string `json:"arbeitnehmer"`
	Personalnummer string `json:"personalnummer"`
	Arbeitgeber    string `json:"arbeitgeber"`
}

// Paar is one label and value in the year-rule block.
type Paar struct {
	Label string `json:"label"`
	Wert  string `json:"wert"`
}

// Zeile is one receipt row, or the totals row when Summe is set.
type Zeile struct {
	Nr          string `json:"nr"`
	Datum       string `json:"datum"`
	Wt          string `json:"wt"`
	Mahlzeit    string `json:"mahlzeit"`
	Bezugsort   string `json:"bezugsort"`
	Arbeitsort  string `json:"arbeitsort"`
	Haendler    string `json:"haendler"`
	Beleg       string `json:"beleg"`
	Anerkannt   string `json:"anerkannt"`
	Erstattung  string `json:"erstattung"`
	Eigenanteil string `json:"eigenanteil"`
	GV          string `json:"gv"`
	Steuerfrei  string `json:"steuerfrei"`
	Regulaer    string `json:"regulaer"`
	Hinweise    string `json:"hinweise"`
	Summe       bool   `json:"summe"`
}

// SummenBlock is the month total section. Amounts are already formatted.
type SummenBlock struct {
	Anzahl         string `json:"anzahl"`
	Belegbetrag    string `json:"belegbetrag"`
	Anerkannt      string `json:"anerkannt"`
	Erstattung     string `json:"erstattung"`
	Eigenanteil    string `json:"eigenanteil"`
	GV             string `json:"gv"`
	Steuerfrei     string `json:"steuerfrei"`
	Regulaer       string `json:"regulaer"`
	Pauschalierung bool   `json:"pauschalierung"`
	Pauschalsteuer string `json:"pauschalsteuer"`
	Soli           string `json:"soli"`
	Kist           string `json:"kist"`
	PauschalGesamt string `json:"pauschal_gesamt"`
	ANPflichtig    string `json:"an_pflichtig"`
	AGKosten       string `json:"ag_kosten"`
	OhnePauschal   string `json:"ohne_pauschal"`
}

// Check is one Prüfpunkt line.
type Check struct {
	Symbol string `json:"symbol"`
	Text   string `json:"text"`
}

// Aenderung is one row of the version-diff table.
type Aenderung struct {
	Zeitpunkt string `json:"zeitpunkt"`
	Bezug     string `json:"bezug"`
	Feld      string `json:"feld"`
	Alt       string `json:"alt"`
	Neu       string `json:"neu"`
	Grund     string `json:"grund"`
}

// AnhangSeite is one receipt-image page.
type AnhangSeite struct {
	Nr             string `json:"nr"`
	Erste          bool   `json:"erste"`
	Datei          string `json:"datei"`
	Datum          string `json:"datum"`
	Wt             string `json:"wt"`
	Haendler       string `json:"haendler"`
	Ort            string `json:"ort"`
	Mahlzeit       string `json:"mahlzeit"`
	Bezugsort      string `json:"bezugsort"`
	Arbeitsort     string `json:"arbeitsort"`
	Belegbetrag    string `json:"belegbetrag"`
	Korrigiert     string `json:"korrigiert"`
	KorrekturGrund string `json:"korrektur_grund"`
	Erstattung     string `json:"erstattung"`
	Quelle         string `json:"quelle"`
	Erkannt        string `json:"erkannt"`
	SHA256         string `json:"sha256"`
	Upload         string `json:"upload"`
	Geaendert      string `json:"geaendert"`
	Seite          string `json:"seite"`
}

// Image is a JPEG written next to the template. Name is relative to the Typst root.
type Image struct {
	Name string
	JPEG []byte
}

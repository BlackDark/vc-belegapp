// Package problem is RFC 9457 application/problem+json for the API.
package problem

import "fmt"

// Field is one felder[] entry.
type Field struct {
	Feld string `json:"feld"`
	Code string `json:"code"`
	Text string `json:"text"`
}

// Error is a client-visible problem. Internal failures stay as plain errors.
type Error struct {
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Status    int     `json:"status"`
	Code      string  `json:"code"`
	Detail    string  `json:"detail,omitempty"`
	Felder    []Field `json:"felder,omitempty"`
	RequestID string  `json:"request_id,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail != "" {
		return e.Code + ": " + e.Detail
	}
	return e.Code
}

// New builds a problem without field errors.
func New(status int, code, detail string) *Error {
	return &Error{
		Type:   "about:blank",
		Title:  title(status),
		Status: status,
		Code:   code,
		Detail: detail,
	}
}

// Fields builds a 422 problem. code is the first field code when fields is non-empty.
func Fields(code, detail string, fields []Field) *Error {
	if code == "" && len(fields) > 0 {
		code = fields[0].Code
	}
	return &Error{
		Type:   "about:blank",
		Title:  title(422),
		Status: 422,
		Code:   code,
		Detail: detail,
		Felder: fields,
	}
}

func title(status int) string {
	switch status {
	case 401:
		return "Nicht angemeldet"
	case 403:
		return "Unzulässig"
	case 404:
		return "Nicht gefunden"
	case 409:
		return "Konflikt"
	case 413:
		return "Datei zu groß"
	case 422:
		return "Validierung fehlgeschlagen"
	case 429:
		return "Zu viele Versuche"
	case 503:
		return "Vorübergehend nicht verfügbar"
	case 501:
		return "Nicht implementiert"
	default:
		return fmt.Sprintf("Fehler %d", status)
	}
}

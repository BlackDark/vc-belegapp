// Package notify delivers outbound HTTP webhooks for events an operator
// switched on. It is deliberately small: one POST of a JSON document, and the
// outcome folded into the retry classes the job queue already understands.
//
// Retries belong to the queue, not to this package. The HTTP client never
// repeats a request on its own, so a recipient sees each event once per job
// attempt and the backoff stays in one place.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Intake sources. They are the payload's filter field: a recipient decides on
// them whether an event matters, because a watched folder can produce hundreds
// of images an evening while a phone upload is worth a message every time.
// The wire values are the ones documented in docs/configuration.md and must
// not change without a payload version bump.
const (
	SourceManual = "manuell"
	SourceMail   = "mail"
	SourceWatch  = "watch"
	// SourceUnknown is what the service sends for a caller that named no source
	// or one this version does not know. It is a value of its own and never a
	// silent "manuell": a recipient filtering on quelle has to be able to tell
	// "nobody said" from "a person uploaded this".
	SourceUnknown = "unknown"
)

// EventBildPruefen is the event name in the payload: an image was stored and no
// Beleg owns it, so it waits in *Offene Bilder* for review.
const EventBildPruefen = "bild.pruefen"

// PayloadVersion is the schema version every document of this package carries.
// A receiver that does not know the version ignores the body instead of
// guessing; a change to the fields below needs a new number.
const PayloadVersion = 1

const (
	// defaultTimeout is what a caller gets when it configures no timeout of its
	// own. A webhook that has not answered in ten seconds will not answer
	// before the operator's phone has moved on.
	defaultTimeout = 10 * time.Second
	// responseLimit bounds how much of the answer is drained. The body is not
	// part of the contract, but reading a prefix lets the connection go back
	// to the pool for the next event instead of being torn down.
	responseLimit = 4 * 1024
	userAgent     = "belegapp"
)

// BildPruefen is the body of the bild.pruefen event.
//
// The document is the whole contract and it is deliberately thin: an id, the
// moment the image landed, the intake it came from, and a deep link into the
// review form. It carries no image bytes, no blob key, no SHA-256, no mailbox
// host or folder, and no credential — a recipient learns that a picture
// arrived and gets an id it can already look up itself, nothing more.
//
// AbschlussURL is a path inside this application, not an absolute link: the
// receiver turns it into a link with its own base URL.
type BildPruefen struct {
	Version      int    `json:"version"`
	Event        string `json:"event"`
	BildID       string `json:"bild_id"`
	ErstelltAm   string `json:"erstellt_am"`
	Quelle       string `json:"quelle"`
	AbschlussURL string `json:"abschluss_url"`
}

// BildPruefenFor builds the bild.pruefen event for one unassigned image.
// erstelltAm is an RFC 3339 stamp as the rest of the application writes it
// (UTC), and quelle is one of the Source constants.
func BildPruefenFor(bildID, erstelltAm, quelle string) BildPruefen {
	return BildPruefen{
		Version:      PayloadVersion,
		Event:        EventBildPruefen,
		BildID:       bildID,
		ErstelltAm:   erstelltAm,
		Quelle:       quelle,
		AbschlussURL: "/belege/neu?bild=" + url.QueryEscape(bildID),
	}
}

// KnownSource reports whether a source string is one this package names. An
// unknown value is not an error here; the service maps it to SourceUnknown,
// because a mislabelled event must never cost a receipt and a machine ingest
// must not arrive wearing a human's label.
func KnownSource(source string) bool {
	switch source {
	case SourceManual, SourceMail, SourceWatch, SourceUnknown:
		return true
	default:
		return false
	}
}

// WebhookOptions configures the outbound webhook.
type WebhookOptions struct {
	// URL is the absolute http(s) address the event is posted to. Empty means
	// the feature is off.
	URL string
	// Token is sent as an Authorization: Bearer header when set.
	Token string
	// Timeout bounds one delivery, including connect, write and read.
	Timeout time.Duration
	// HTTPClient overrides the client. Tests use it; production does not.
	HTTPClient *http.Client
}

// Webhook posts JSON documents to one operator-configured address.
type Webhook struct {
	url     string
	token   string
	timeout time.Duration
	client  *http.Client
}

// NewWebhook builds the client. An empty URL is not an error: the caller reads
// Configured and skips the feature, which is what keeps an instance without a
// webhook from ever building a payload.
func NewWebhook(opt WebhookOptions) *Webhook {
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	client := opt.HTTPClient
	if client == nil {
		client = &http.Client{
			// No retries here on purpose. The job queue retries a failed job
			// with its own backoff and attempt limit; a second attempt inside
			// the client would multiply the notifications a recipient sees.
			Transport: http.DefaultTransport,
		}
	}
	return &Webhook{
		url:     strings.TrimSpace(opt.URL),
		token:   strings.TrimSpace(opt.Token),
		timeout: timeout,
		client:  client,
	}
}

// Configured reports whether the operator set an address. Every caller checks
// it before doing any work, which is what makes an unconfigured instance cost
// nothing.
func (w *Webhook) Configured() bool {
	return w != nil && w.url != ""
}

// Send posts one event and classifies the outcome.
//
// A 2xx is delivered. A 5xx, a 408, a 429, a timeout, or a transport error is
// transient, because the receiver either is not answering or asked to come
// back. Every other 4xx is permanent: the receiver answered and decided, and
// the identical retry would be decided the same way.
//
// The returned error never mentions the address, the answer, or the token —
// the queue writes it into the job row and into the log.
func (w *Webhook) Send(ctx context.Context, payload BildPruefen) error {
	if !w.Configured() {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return &Error{Kind: KindPermanent, Text: "Benachrichtigung nicht darstellbar"}
	}
	// An explicit deadline rather than http.Client.Timeout, so a slow receiver
	// costs this job its time and not the connection's.
	callCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		// The error of NewRequest names the URL, which may carry an ntfy
		// token in its path. It is replaced, not wrapped.
		return &Error{Kind: KindPermanent, Text: "Webhook-Adresse ungültig"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return asError(err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, responseLimit))
		_ = resp.Body.Close()
	}()
	return asStatus(resp.StatusCode)
}

// asStatus maps the HTTP status onto a retry class.
func asStatus(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusRequestTimeout, status == http.StatusTooManyRequests:
		return &Error{Kind: KindTransient, Text: "Empfänger vorübergehend nicht erreichbar"}
	case status >= 500:
		return &Error{Kind: KindTransient, Text: "Empfänger vorübergehend nicht erreichbar"}
	default:
		return &Error{Kind: KindPermanent, Text: "Webhook abgelehnt"}
	}
}

// asError folds a transport error into a retry class. Every branch returns a
// fixed sentence; url.Error would name the address and the token inside it.
func asError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{Kind: KindTransient, Text: "Zeitüberschreitung"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &Error{Kind: KindTransient, Text: "Zeitüberschreitung"}
	}
	return &Error{Kind: KindTransient, Text: "Empfänger nicht erreichbar"}
}

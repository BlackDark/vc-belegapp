package mailinbox

import (
	"context"
	"errors"
	"net"

	"github.com/emersion/go-imap/v2"
)

// Attachment is one MIME part of a message that the server marked as an
// attachment.
//
// MimeType is the canonical declared media type and is set whenever the part
// carried a usable Content-Type, even for a type this application will not
// store, so the caller can log what arrived. Data holds the bytes only for the
// picture types the source reads; it is nil for anything else, and it is capped
// at one byte more than the source was told to allow, so the caller can tell
// "at the cap" from "over the cap". Err is set only when the transfer itself
// failed. Usable says whether there is something to store at all.
type Attachment struct {
	Filename string
	MimeType string
	Data     []byte
	Err      error
}

// Usable reports whether this attachment is a decoded part with bytes behind it.
func (a Attachment) Usable() bool {
	return a.Err == nil && a.MimeType != "" && len(a.Data) > 0
}

// Message is one mail with the attachments that could be read. UIDs are stable
// inside a mailbox and are what MarkIngested and Move address.
type Message struct {
	UID         imap.UID
	From        string
	Subject     string
	Attachments []Attachment
}

// Source is a mailbox the service can poll. Implementations must never delete
// messages.
type Source interface {
	// Fetch returns up to limit messages that carry no marker of ours,
	// oldest first. It reports how many it could not read; those stay in the
	// mailbox and the next poll tries again.
	Fetch(ctx context.Context, limit int) ([]Message, error)
	// MarkIngested flags the messages so the next poll skips them. It never
	// sets \Deleted.
	MarkIngested(ctx context.Context, uids []imap.UID) error
	// Move copies the messages into folder and removes them from the polled
	// mailbox. An empty folder is a no-op.
	Move(ctx context.Context, uids []imap.UID, folder string) error
}

// Retry classes. The job queue retries transient failures, gives up on auth and
// permanent ones, and re-runs schema ones once. They mirror the values the
// recognition package already uses so one queue serves both.
const (
	KindTransient = "transient"
	KindSchema    = "schema"
	KindAuth      = "auth"
	KindPermanent = "permanent"
)

// Error is a mailbox failure with a retry class and a short German text.
//
// Error() returns Text and nothing else. That text reaches the log and the
// jobs.fehler column, and the wrapped transport error names what it names:
// net.OpError carries "dial tcp imap.example.de:993", and an *imap.Error
// carries the server response verbatim. Neither belongs in a row that outlives
// the process, and internal/notify already refuses exactly this for the webhook
// address (RedactURL, asError). The cause stays reachable through Unwrap for
// errors.Is and errors.As; it is deliberately absent from the printed form.
type Error struct {
	Kind string
	Text string
	Err  error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Text
}

// Unwrap exposes the underlying transport error to errors.Is and errors.As.
// Nothing prints it: the job row and the log see Error(), which is Text alone.
func (e *Error) Unwrap() error { return e.Err }

// RetryKind is what the job queue reads to decide between another attempt and
// a permanent failure. An unset Kind is transient, the safe default for a
// network that misbehaved.
func (e *Error) RetryKind() string {
	if e == nil || e.Kind == "" {
		return KindTransient
	}
	return e.Kind
}

func transient(text string, err error) *Error {
	return &Error{Kind: KindTransient, Text: text, Err: err}
}

func authFailed(text string, err error) *Error {
	return &Error{Kind: KindAuth, Text: text, Err: err}
}

func permanent(text string, err error) *Error {
	return &Error{Kind: KindPermanent, Text: text, Err: err}
}

// classify maps a transport error onto a retry class, and returns nil for a nil
// error. It returns the error interface rather than *Error on purpose: a typed
// nil pointer would compare non-nil at every call site and turn a clean poll
// into a failure.
//
// A mailbox rejects a login with NO [AUTHENTICATIONFAILED]: a wrong password or
// a disabled account will still be rejected in five minutes, so retrying it
// only fills the queue with doomed jobs. A BAD response is a client or server
// bug — an unknown command, a flag the server will not store — and repeating
// the request cannot change it. Everything else is the network or the far side
// being briefly unavailable, which is exactly what a retry is for.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var call *Error
	if errors.As(err, &call) {
		return call
	}
	var resp *imap.Error
	if errors.As(err, &resp) {
		switch {
		case resp.Code == imap.ResponseCodeAuthenticationFailed:
			return authFailed("Zugangsdaten für das Postfach werden abgelehnt", err)
		case resp.Type == imap.StatusResponseTypeBad:
			return permanent("Der Mailserver lehnt die Anfrage ab", err)
		}
		return transient("Der Mailserver antwortet mit einem Fehler", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return transient("Zeitüberschreitung beim Postfach", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return transient("Der Mailserver ist nicht erreichbar", err)
	}
	return transient("Die Verbindung zum Postfach ist fehlgeschlagen", err)
}

// retryKind is the retry class of any error, for the caller that only branches
// on it and does not report the error itself.
func retryKind(err error) string {
	if err == nil {
		return ""
	}
	var kind interface{ RetryKind() string }
	if errors.As(err, &kind) {
		return kind.RetryKind()
	}
	return KindTransient
}

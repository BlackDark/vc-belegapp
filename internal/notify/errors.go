package notify

// Retry classes for the job queue. The values are the ones internal/jobs
// understands; they are spelled out here so this package does not have to
// import the recognition errors to say how a delivery failed.
const (
	// KindTransient is worth another attempt: a timeout, a refused connection,
	// a 5xx, or a 429 that asks the caller to come back.
	KindTransient = "transient"
	// KindPermanent will not change on a second attempt: a rejected address, a
	// 4xx the receiver decided on, or a payload it refuses.
	KindPermanent = "permanent"
)

// Error is a failed delivery with a retry class.
//
// Text is a fixed German sentence and never carries the URL, the response
// body, or the bearer token. The queue stores it in the job row and prints it
// in the log, and an ntfy token can sit in the path of the URL, so nothing
// from the request may end up here.
type Error struct {
	Kind string
	Text string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Text
}

// RetryKind reports how the job queue should treat the error.
func (e *Error) RetryKind() string {
	if e == nil || e.Kind == "" {
		return KindTransient
	}
	return e.Kind
}

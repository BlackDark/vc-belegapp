package erkennung

// Retry classes for the job queue. Values are stable for logs and tests.
const (
	KindTransient = "transient"
	KindSchema    = "schema"
	KindAuth      = "auth"
	KindPermanent = "permanent"
)

// CallError is a recognition failure with a user-facing text and a retry class.
// Text never includes image bytes or API keys.
type CallError struct {
	Kind string
	Text string
}

func (e *CallError) Error() string {
	if e == nil {
		return ""
	}
	return e.Text
}

// RetryKind reports how the job queue should treat the error.
func (e *CallError) RetryKind() string {
	if e == nil || e.Kind == "" {
		return KindTransient
	}
	return e.Kind
}

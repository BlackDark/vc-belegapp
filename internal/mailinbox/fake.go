package mailinbox

import (
	"context"
	"sync"

	"github.com/emersion/go-imap/v2"
)

// Fake is an in-memory Source. The service suites drive IngestMailbox through
// it instead of standing up an IMAP server; it records the calls so a test can
// assert the disposition of every message.
//
// Messages already marked through MarkIngested are dropped from the next
// Fetch, which is what a real server does with the keyword flag. That is the
// idempotency the service relies on.
type Fake struct {
	// Messages is the queue, oldest first. Fetch copies it.
	Messages []Message
	// FetchErr, MarkErr and MoveErr fail the matching call.
	FetchErr error
	MarkErr  error
	MoveErr  error
	// Limit caps what Fetch returns even if the caller asked for more.
	Limit int
	// UIDValidityValue is the mailbox epoch this fake stands for. Zero (the
	// default) means the source tracks none, and the polled note is left alone;
	// a test that raises it between polls simulates a server that rebuilt the
	// mailbox and reassigned every UID.
	UIDValidityValue uint32

	mu       sync.Mutex
	marked   map[imap.UID]bool
	movedUID map[imap.UID]bool
	// Seen and Moved are the UID lists of the last successful call.
	Seen []imap.UID
	// MovedTo is the folder of the last successful Move.
	MovedTo string
	// MoveCalls counts every Move, including the ones with no UID.
	MoveCalls int
}

// Fetch returns the messages no earlier call has marked.
func (f *Fake) Fetch(_ context.Context, limit int) ([]Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FetchErr != nil {
		return nil, f.FetchErr
	}
	if f.Limit > 0 && limit > f.Limit {
		limit = f.Limit
	}
	out := make([]Message, 0, len(f.Messages))
	for _, msg := range f.Messages {
		if f.marked[msg.UID] {
			continue
		}
		out = append(out, msg)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// MarkIngested records the UIDs and takes them out of the queue. Earlier marks
// stay marked: the real marker keyword on the server is cumulative, and a fake
// that forgot them would hide a message the poll had already consumed.
func (f *Fake) MarkIngested(_ context.Context, uids []imap.UID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MarkErr != nil {
		return f.MarkErr
	}
	if f.marked == nil {
		f.marked = map[imap.UID]bool{}
	}
	for _, uid := range uids {
		f.marked[uid] = true
	}
	f.Seen = append([]imap.UID(nil), uids...)
	return nil
}

// Move records the destination.
func (f *Fake) Move(_ context.Context, uids []imap.UID, folder string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.MoveCalls++
	if f.MoveErr != nil {
		return f.MoveErr
	}
	if f.movedUID == nil {
		f.movedUID = map[imap.UID]bool{}
	}
	for _, uid := range uids {
		f.movedUID[uid] = true
	}
	f.MovedTo = folder
	return nil
}

// MovedUID reports whether a UID was moved.
func (f *Fake) MovedUID(uid imap.UID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.movedUID[uid]
}

// UIDValidity reports the mailbox epoch this fake stands for.
func (f *Fake) UIDValidity() uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.UIDValidityValue
}

// Reset forgets every mark, as a fresh process would see the same mailbox.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = nil
	f.movedUID = nil
	f.Seen = nil
	f.MovedTo = ""
	f.MoveCalls = 0
}

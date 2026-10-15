package mailinbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-message/mail"
)

// memServer is go-imap's own in-memory server reached over an in-process
// pipe. It speaks the real protocol, so this exercises search, fetch, store,
// and move against genuine server behaviour rather than a mock of it. No socket
// is opened: both ends come from net.Pipe.
type memServer struct {
	ln      *pipeListener
	server  *imapserver.Server
	mem     *imapmemserver.Server
	user    *imapmemserver.User
	serveWG sync.WaitGroup
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// pipeListener hands out the server ends of net.Pipe connections. The IMAP
// server accepts on it like any other listener.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
	// log, when set, tees the bytes the client sends into it. A test that
	// cares about the wire command rather than about a Go struct sets it
	// before it builds the source.
	log *commandLog
}

// commandLog records what a client wrote. Every write goes through one mutex,
// because the observer client writes from the test goroutine while the source
// client writes from its own session goroutine.
type commandLog struct {
	mu   sync.Mutex
	text bytes.Buffer
}

func (l *commandLog) add(p []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.text.Write(p)
}

func (l *commandLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

// recordedConn is the client end of the pipe with its writes recorded.
type recordedConn struct {
	net.Conn
	log *commandLog
}

func (c *recordedConn) Write(p []byte) (int, error) {
	c.log.add(p)
	return c.Conn.Write(p)
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

// pipeDialer wires one client connection to one server connection.
func (l *pipeListener) dialer(_ string, options *imapclient.Options) (*imapclient.Client, error) {
	clientConn, serverConn := net.Pipe()
	select {
	case l.conns <- serverConn:
	case <-l.closed:
		return nil, net.ErrClosed
	}
	if l.log != nil {
		clientConn = &recordedConn{Conn: clientConn, log: l.log}
	}
	return imapclient.New(clientConn, options), nil
}

func newMemServer(t *testing.T, username, password string, caps imap.CapSet) *memServer {
	t.Helper()
	return newMemServerWith(t, username, password, caps, nil)
}

// newMemServerWith optionally wraps the mem server session, which is how a
// test simulates a server that lacks a capability.
func newMemServerWith(t *testing.T, username, password string, caps imap.CapSet, wrap func(imapserver.Session) imapserver.Session) *memServer {
	t.Helper()
	mem := imapmemserver.New()
	user := imapmemserver.NewUser(username, password)
	mem.AddUser(user)
	if caps == nil {
		caps = imap.CapSet{imap.CapIMAP4rev1: {}}
	}
	ln := newPipeListener()
	ms := &memServer{ln: ln, mem: mem, user: user}
	ms.server = imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			session := mem.NewSession()
			if wrap != nil {
				session = wrap(session)
			}
			return session, nil, nil
		},
		// InsecureAuth is the mem server's own stand-in for TLS: the pipe is
		// already a private, in-process connection, and the suite is about
		// protocol behaviour, not about the handshake.
		InsecureAuth: true,
		Caps:         caps,
		Logger:       testLogger{},
	})
	ms.serveWG.Add(1)
	go func() {
		defer ms.serveWG.Done()
		_ = ms.server.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = ms.server.Close()
		_ = ln.Close()
		ms.serveWG.Wait()
	})
	return ms
}

type testLogger struct{}

func (testLogger) Printf(string, ...any) {}

// testTime keeps the fixture deterministic; the Date header carries the same
// instant.
var testTime = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// source builds an IMAP source pointed at the in-process server.
func (ms *memServer) source(t *testing.T, options Options) *IMAP {
	t.Helper()
	options.Host = "pipe"
	options.Port = 993
	if options.Inbox == "" {
		options.Inbox = "INBOX"
	}
	if options.Username == "" {
		options.Username = "belege"
	}
	if options.Password == "" {
		options.Password = "app-password"
	}
	options.TLS = false
	options.STARTTLS = false
	src := New(options)
	if src == nil {
		t.Fatal("New returned nil")
	}
	src.dial = ms.ln.dialer
	return src
}

func (ms *memServer) createMailbox(t *testing.T, name string) {
	t.Helper()
	if err := ms.user.Create(name, nil); err != nil {
		t.Fatal(err)
	}
}

// bytesLiteral is the imap.LiteralReader the mem server's Append wants.
type bytesLiteral struct {
	*bytes.Reader
	size int64
}

func newLiteral(raw []byte) *bytesLiteral {
	return &bytesLiteral{Reader: bytes.NewReader(raw), size: int64(len(raw))}
}

func (l *bytesLiteral) Size() int64 { return l.size }

// appendMessage puts one RFC 5322 message into a folder. The mem server
// dereferences the options without a nil check, so it always gets one.
func (ms *memServer) appendMessage(t *testing.T, mailbox string, raw []byte) imap.UID {
	t.Helper()
	data, err := ms.user.Append(mailbox, newLiteral(raw), &imap.AppendOptions{Time: testTime})
	if err != nil {
		t.Fatal(err)
	}
	return data.UID
}

// messageWith builds a multipart mail with one picture attachment, one PDF, and
// a text body — the exact shape a forwarded receipt has.
func messageWithImage(from, subject string, picture []byte, mimeType, filename string, extra map[string]string) []byte {
	return messageWithPictures(from, subject, []testPicture{
		{Data: picture, MimeType: mimeType, Filename: filename},
	}, extra)
}

// testPicture is one attachment part of the builder below.
type testPicture struct {
	Data     []byte
	MimeType string
	Filename string
}

// messageWithPictures builds a multipart mail with n picture parts and a text
// body. A message with two hundred parts is the shape the per-part cap alone
// never sees, so the fixture has to be able to produce one.
func messageWithPictures(from, subject string, pictures []testPicture, extra map[string]string) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: belege@example.de\r\n")
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "Date: Thu, 08 Oct 2026 12:00:00 +0200\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	for key, value := range extra {
		fmt.Fprintf(&buf, "%s: %s\r\n", key, value)
	}
	boundary := "----=_belegapp_test"
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary)

	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	buf.WriteString("Beleg im Anhang.\r\n")

	for _, picture := range pictures {
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: %s; name=%q\r\n", picture.MimeType, picture.Filename)
		buf.WriteString("Content-Transfer-Encoding: base64\r\n")
		fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=%q\r\n\r\n", picture.Filename)
		encoded := base64.StdEncoding.EncodeToString(picture.Data)
		for i := 0; i < len(encoded); i += 76 {
			end := i + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			buf.WriteString(encoded[i:end])
			buf.WriteString("\r\n")
		}
	}
	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes()
}

// pictureBytes is n bytes that are not a real picture. The source decodes the
// transfer encoding and hands the bytes on without looking inside a part, so
// the cap tests need volume rather than an image — and this keeps them small.
func pictureBytes(n int, fill byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = fill
	}
	return out
}

// pngBytes is a minimal valid 1x1 PNG.
func pngBytes() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
		0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
		0x0d, 0x0a, 0x2d, 0xb4,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
}

// The whole round trip against the real protocol: login, search, fetch with
// MIME decoding, the image kept and the PDF dropped.
func TestIMAPFetchRealServer(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	ms.appendMessage(t, "INBOX", messageWithImage(
		"kasse@example.de", "Kassenbon",
		pngBytes(), "image/png", "beleg.png",
		map[string]string{"X-Mailer": "Telefon"},
	))

	src := ms.source(t, Options{MaxAttachmentBytes: 1 << 20})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages %d", len(messages))
	}
	msg := messages[0]
	if msg.From != "kasse@example.de" {
		t.Fatalf("from %q", msg.From)
	}
	if msg.UID == 0 {
		t.Fatal("no UID")
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments %d, want only the PNG", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.MimeType != "image/png" || att.Filename != "beleg.png" {
		t.Fatalf("attachment %+v", att)
	}
	if !bytes.Equal(att.Data, pngBytes()) {
		t.Fatalf("attachment bytes differ (%d)", len(att.Data))
	}
}

// A PDF attachment is reported with its declared type but without bytes: the
// source never reads a body it is going to hand back unusable, and the service
// rejects it on the type.
func TestIMAPFetchSkipsPDFBody(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	ms.appendMessage(t, "INBOX", messageWithImage(
		"kasse@example.de", "Kassenbon als PDF",
		[]byte("%PDF-1.7\n%%EOF\n"), "application/pdf", "beleg.pdf", nil,
	))

	src := ms.source(t, Options{})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages %d", len(messages))
	}
	if len(messages[0].Attachments) != 1 {
		t.Fatalf("attachments %d, want the PDF reported", len(messages[0].Attachments))
	}
	att := messages[0].Attachments[0]
	if att.MimeType != "application/pdf" {
		t.Fatalf("declared type %q, want application/pdf", att.MimeType)
	}
	if att.Usable() || len(att.Data) != 0 {
		t.Fatalf("the PDF body was read: %+v", att)
	}
}

// The idempotency contract end to end: after MarkIngested the keyword probe
// makes the next Fetch skip the message, so a second poll produces nothing.
func TestIMAPFetchIsIdempotentAfterMarkIngested(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))
	ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon 2", pngBytes(), "image/png", "b2.png", nil))

	src := ms.source(t, Options{})
	first, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("first fetch %d", len(first))
	}
	uids := []imap.UID{first[0].UID, first[1].UID}
	if err := src.MarkIngested(ctx, uids); err != nil {
		t.Fatal(err)
	}

	second, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second fetch returned %d messages; the marker did not stick", len(second))
	}
}

// A rejected sender's mail has to come out of the poll exactly as it went in.
// RFC 3501 §6.4.8 makes a plain BODY fetch set \Seen as a side effect, so the
// command on the wire has to be BODY.PEEK[]: without it the poll would mark
// every stranger's mail read before the allowlist ever saw it, which is the one
// thing docs/configuration.md promises it does not do.
func TestIMAPFetchUsesPeekAndSetsNoFlag(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	wire := &commandLog{}
	ms.ln.log = wire
	ms.createMailbox(t, "INBOX")
	uid := ms.appendMessage(t, "INBOX", messageWithImage(
		"fremd@example.de", "Kassenbon", pngBytes(), "image/png", "beleg.png", nil,
	))

	src := ms.source(t, Options{})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].UID != uid {
		t.Fatalf("fetch %+v", messages)
	}

	sent := wire.String()
	if !strings.Contains(sent, "BODY.PEEK[]") {
		t.Fatalf("the poll did not ask for BODY.PEEK[]: %q", sent)
	}
	if strings.Contains(sent, "BODY[]") {
		t.Fatalf("a plain BODY[] fetch sets \\Seen on every message it reads: %q", sent)
	}
	if flags := ms.flags(t, "INBOX", uid); len(flags) != 0 {
		t.Fatalf("reading set the flags %v on a message the poll has not consumed", flags)
	}
}

// The two dispositions of one poll against the real protocol: the allowed
// message is flagged, and the stranger's is left untouched — no flags, still in
// the queue for the operator, and read again on the next poll without having
// been consumed.
func TestIMAPRejectedSenderMessageStaysUntouched(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	rejected := ms.appendMessage(t, "INBOX", messageWithImage(
		"fremd@example.de", "Kassenbon", pngBytes(), "image/png", "fremd.png", nil))
	accepted := ms.appendMessage(t, "INBOX", messageWithImage(
		"kasse@example.de", "Kassenbon", pngBytes(), "image/png", "kasse.png", nil))

	src := ms.source(t, Options{})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("fetch %d messages", len(messages))
	}

	// What IngestMailbox does: the allowlist first, then the disposition for
	// everything that was not rejected.
	var seen []imap.UID
	for _, msg := range messages {
		if msg.From != "kasse@example.de" {
			continue
		}
		seen = append(seen, msg.UID)
	}
	if err := src.MarkIngested(ctx, seen); err != nil {
		t.Fatal(err)
	}

	if flags := ms.flags(t, "INBOX", rejected); len(flags) != 0 {
		t.Fatalf("the rejected message carries the flags %v; it has to stay untouched", flags)
	}
	if !hasFlag(ms.flags(t, "INBOX", accepted), Marker) {
		t.Fatal("the accepted message was not flagged")
	}
	if n := ms.messageCount(t, "INBOX"); n != 2 {
		t.Fatalf("messages in INBOX %d, want 2; nothing may be deleted", n)
	}

	second, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].UID != rejected {
		t.Fatalf("second fetch %+v, want only the rejected message again", second)
	}
	if flags := ms.flags(t, "INBOX", rejected); len(flags) != 0 {
		t.Fatalf("the second poll touched the rejected message: %v", flags)
	}
}

// The marker is \Seen plus $Belegapp; nothing else is touched, so the message
// stays readable in the operator's own client.
func TestIMAPMarkIngestedSetsSeenAndKeywordOnly(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	uid := ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))

	src := ms.source(t, Options{})
	if err := src.MarkIngested(ctx, []imap.UID{uid}); err != nil {
		t.Fatal(err)
	}

	flags := ms.flags(t, "INBOX", uid)
	if !hasFlag(flags, imap.FlagSeen) {
		t.Fatalf("flags %v, want \\Seen", flags)
	}
	if !hasFlag(flags, Marker) {
		t.Fatalf("flags %v, want %s", flags, Marker)
	}
	if hasFlag(flags, imap.FlagDeleted) {
		t.Fatalf("flags %v: \\Deleted was set", flags)
	}
	// The message is still there. Nothing was expunged.
	if n := ms.messageCount(t, "INBOX"); n != 1 {
		t.Fatalf("messages in INBOX %d, want 1", n)
	}
}

// hasFlag compares case-insensitively: IMAP flag names are not case-sensitive
// and servers normalise them, so "$Belegapp" comes back as "$belegapp".
func hasFlag(flags map[imap.Flag]bool, want imap.Flag) bool {
	for flag, set := range flags {
		if set && strings.EqualFold(string(flag), string(want)) {
			return true
		}
	}
	return false
}

// With a done folder the message is copied out and the source keeps the copy
// the operator filed. Nothing is deleted from the destination.
func TestIMAPMoveToDoneFolder(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}})
	ms.createMailbox(t, "INBOX")
	ms.createMailbox(t, "Belege/erledigt")
	uid := ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))

	src := ms.source(t, Options{})
	if err := src.MarkIngested(ctx, []imap.UID{uid}); err != nil {
		t.Fatal(err)
	}
	if err := src.Move(ctx, []imap.UID{uid}, "Belege/erledigt"); err != nil {
		t.Fatal(err)
	}
	if n := ms.messageCount(t, "Belege/erledigt"); n != 1 {
		t.Fatalf("messages in the done folder %d", n)
	}
	if n := ms.messageCount(t, "INBOX"); n != 0 {
		t.Fatalf("messages left in INBOX %d", n)
	}
}

// MOVE is the only way a message leaves a folder here. A server without the
// capability is reported instead of falling back to COPY + \Deleted + EXPUNGE.
func TestIMAPMoveWithoutCapabilityIsPermanent(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	ms.createMailbox(t, "Belege/erledigt")
	uid := ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))

	src := ms.source(t, Options{})
	err := src.Move(ctx, []imap.UID{uid}, "Belege/erledigt")
	if err == nil {
		t.Fatal("expected the missing MOVE capability to be reported")
	}
	var call *Error
	if !asError(err, &call) {
		t.Fatalf("error %v carries no retry class", err)
	}
	if call.RetryKind() != KindPermanent {
		t.Fatalf("retry kind %q, want %q", call.RetryKind(), KindPermanent)
	}
	// The message stayed in the inbox; no fallback deleted it.
	if n := ms.messageCount(t, "INBOX"); n != 1 {
		t.Fatalf("messages in INBOX %d, want 1", n)
	}
}

// A wrong password is an auth failure: the job queue must not retry it, and
// the password itself reaches neither the error nor the log.
func TestIMAPWrongPasswordIsAuth(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	const secret = "s3cr3t-app-passwort-4711"

	var buf bytes.Buffer
	src := ms.source(t, Options{Password: secret, Log: slog.New(slog.NewJSONHandler(&buf, nil))})
	_, err := src.Fetch(ctx, 10)
	if err == nil {
		t.Fatal("expected a login failure")
	}
	var call *Error
	if !asError(err, &call) {
		t.Fatalf("error %v carries no retry class", err)
	}
	if call.RetryKind() != KindAuth {
		t.Fatalf("retry kind %q, want %q", call.RetryKind(), KindAuth)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error leaks the password: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("the log leaks the password: %s", buf.String())
	}
}

// A server that is not there is transient, so the queue tries again. The
// address the dial failed on is not part of that answer: it would be stored in
// the job row and printed in the log.
func TestIMAPDialFailureIsTransient(t *testing.T) {
	ctx := context.Background()
	src := New(Options{Host: "imap.example.de", Port: 993, Username: "belege", Password: "x", Timeout: 50 * time.Millisecond})
	src.dial = func(string, *imapclient.Options) (*imapclient.Client, error) {
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("dial tcp imap.example.de:993: connection refused")}
	}
	_, err := src.Fetch(ctx, 10)
	var call *Error
	if !asError(err, &call) {
		t.Fatalf("error %v carries no retry class", err)
	}
	if call.RetryKind() != KindTransient {
		t.Fatalf("retry kind %q, want %q", call.RetryKind(), KindTransient)
	}
	if strings.Contains(err.Error(), "imap.example.de") {
		t.Fatalf("the error repeats the mailbox address: %s", err.Error())
	}
}

// An attachment over the cap is truncated to cap+1 bytes, never to cap, so the
// caller can tell "at the limit" from "over it".
func TestIMAPAttachmentCap(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	ms.appendMessage(t, "INBOX", messageWithImage(
		"kasse@example.de", "Riesiges Bild", pngBytes(), "image/png", "gross.png", nil,
	))

	src := ms.source(t, Options{MaxAttachmentBytes: 10})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || len(messages[0].Attachments) != 1 {
		t.Fatalf("messages %+v", messages)
	}
	if got := len(messages[0].Attachments[0].Data); got != 11 {
		t.Fatalf("attachment read %d bytes, want cap+1", got)
	}
}

// Without RFC 5258 keywords the server rejects the probe with BAD and the poll
// falls back to \Seen. The idempotency has to survive that path too, or a
// poll on such a server would duplicate every receipt.
func TestIMAPFallbackToSeenWithoutKeywords(t *testing.T) {
	ctx := context.Background()
	// noKeywordsSession answers BAD to any SEARCH that names a flag.
	ms := newMemServerWith(t, "belege", "app-password", nil, withoutKeywords)
	ms.createMailbox(t, "INBOX")
	ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))

	src := ms.source(t, Options{})
	first, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("first fetch %d", len(first))
	}
	if err := src.MarkIngested(ctx, []imap.UID{first[0].UID}); err != nil {
		t.Fatal(err)
	}
	second, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second fetch %d messages; the \\Seen fallback did not stick", len(second))
	}
}

// noKeywordsSession refuses a SEARCH that names a keyword, the way a
// pre-RFC 5258 server does: \Seen is from RFC 3501 and every server has it, an
// arbitrary $keyword is from RFC 5258 and an older one answers BAD.
type noKeywordsSession struct {
	imapserver.Session
}

func (s noKeywordsSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	for _, flag := range append(append([]imap.Flag{}, criteria.Flag...), criteria.NotFlag...) {
		if !strings.HasPrefix(string(flag), `\`) {
			return nil, &imap.Error{Type: imap.StatusResponseTypeBad, Text: "Unsupported search key: FLAG"}
		}
	}
	return s.Session.Search(kind, criteria, options)
}

func withoutKeywords(session imapserver.Session) imapserver.Session {
	return noKeywordsSession{Session: session}
}

func refusingKeywordStore(session imapserver.Session) imapserver.Session {
	return refuseKeywordStore{Session: session}
}

// A server that searches keywords but refuses to store one is the one case
// where neither marker can be persisted. The next poll searches on the keyword
// alone, so reporting success here would hand the same message back on every
// poll and ingest that receipt again and again. The failed store is reported,
// this process stops trusting the keyword, and the message leaves the queue on
// \Seen — so the second poll is empty.
func TestIMAPMarkIngestedReportsRefusedKeywordStore(t *testing.T) {
	ctx := context.Background()
	ms := newMemServerWith(t, "belege", "app-password", nil, refusingKeywordStore)
	ms.createMailbox(t, "INBOX")
	uid := ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))

	src := ms.source(t, Options{})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].UID != uid {
		t.Fatalf("fetch %+v", messages)
	}

	err = src.MarkIngested(ctx, []imap.UID{uid})
	if err == nil {
		t.Fatal("a store that persisted neither marker must not report success")
	}
	var call *Error
	if !asError(err, &call) {
		t.Fatalf("error %v carries no retry class", err)
	}
	if call.RetryKind() != KindTransient {
		t.Fatalf("retry kind %q, want %q", call.RetryKind(), KindTransient)
	}
	if strings.Contains(err.Error(), "app-password") {
		t.Fatalf("the error leaks the password: %v", err)
	}

	flags := ms.flags(t, "INBOX", uid)
	if !hasFlag(flags, imap.FlagSeen) {
		t.Fatalf("flags %v, want \\Seen so the message leaves the queue", flags)
	}
	if hasFlag(flags, imap.FlagDeleted) {
		t.Fatalf("flags %v: \\Deleted was set", flags)
	}

	second, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second fetch returned %d messages; the receipt would be ingested again on every poll", len(second))
	}
}

// The store that refuses the keyword only has to be discovered once. From then
// on the probe skips the question, the poll searches on \Seen alone, and every
// MarkIngested that still carried the keyword would be a command the server
// answers with BAD and a warning to the operator — once per poll, forever, for
// a marker nothing reads any more.
func TestIMAPMarkIngestedStopsAskingForAKeywordItKnowsItCannotStore(t *testing.T) {
	ctx := context.Background()
	ms := newMemServerWith(t, "belege", "app-password", nil, refusingKeywordStore)
	wire := &commandLog{}
	ms.ln.log = wire
	ms.createMailbox(t, "INBOX")
	for i := 0; i < 2; i++ {
		ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))
	}

	src := ms.source(t, Options{})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("fetched %d messages", len(messages))
	}
	// The first store is the one that has to discover the problem: the probe
	// said the keyword works, the store contradicts it.
	if err := src.MarkIngested(ctx, []imap.UID{messages[0].UID}); err == nil {
		t.Fatal("a store that persisted neither marker must not report success")
	}

	// The next message arrives, and its disposition is the second poll of a
	// process that now knows the keyword is unusable.
	ms.ln.log = &commandLog{}
	wire = ms.ln.log
	if err := src.MarkIngested(ctx, []imap.UID{messages[1].UID}); err != nil {
		t.Fatalf("the \\Seen fallback must succeed: %v", err)
	}
	for _, command := range strings.Split(wire.String(), "\r\n") {
		if strings.Contains(command, string(Marker)) {
			t.Fatalf("the poll sent %q again after the keyword proved unstorable", command)
		}
	}

	// And the message still leaves the queue, which is the whole point of the
	// downgraded store.
	remaining, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("fetch returned %d messages; the \\Seen store did not leave the queue", len(remaining))
	}
}

// refuseKeywordStore rejects a STORE that carries a keyword but keeps the
// search working, which is what an inconsistent server looks like.
type refuseKeywordStore struct {
	imapserver.Session
}

func (s refuseKeywordStore) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	for _, flag := range flags.Flags {
		if !strings.HasPrefix(string(flag), `\`) {
			return &imap.Error{Type: imap.StatusResponseTypeBad, Text: "Cannot store that flag"}
		}
	}
	return s.Session.Store(w, numSet, flags, options)
}

// Fetch caps the batch so a large backlog is drained over several polls.
func TestIMAPFetchRespectsLimit(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	for i := 0; i < 3; i++ {
		ms.appendMessage(t, "INBOX", messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))
	}
	src := ms.source(t, Options{})
	messages, err := src.Fetch(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("fetched %d messages, want the limit of 2", len(messages))
	}
}

// The per-part cap bounds one part and nothing else. readBody drains every part
// to reach the next, so a message of two hundred 15 MB JPEGs is three gigabytes
// of heap before the caller sees a single attachment — and the caller, which
// only bounds what it stores, sees all of them at once. The sum is what has to
// stop it, while the message is being decoded.
//
// What is over the sum is not the message: the two pictures behind it are
// complete, so they are kept, and the bound itself arrives as one more part
// carrying the reason and no bytes. The caller skips that one like any other
// part it cannot store.
func TestIMAPMessageByteCapStopsDecoding(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	const partBytes = 1000
	pictures := []testPicture{
		{Data: pictureBytes(partBytes, 'a'), MimeType: "image/jpeg", Filename: "1.jpg"},
		{Data: pictureBytes(partBytes, 'b'), MimeType: "image/jpeg", Filename: "2.jpg"},
		{Data: pictureBytes(partBytes, 'c'), MimeType: "image/jpeg", Filename: "3.jpg"},
		{Data: pictureBytes(partBytes, 'd'), MimeType: "image/jpeg", Filename: "4.jpg"},
	}
	ms.appendMessage(t, "INBOX", messageWithPictures("kasse@example.de", "Viele Belege", pictures, nil))

	// Room for two parts and 500 bytes: the third does not fit and stops the
	// decode. The per-part cap sits above the part size and below the total —
	// below, because a total under the per-part cap is raised to it, so the sum
	// stays the bound under test rather than a value New has already moved.
	src := ms.source(t, Options{MaxAttachmentBytes: 1200, MaxMessageBytes: 2*partBytes + 500})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages %+v", messages)
	}
	got := messages[0].Attachments
	if len(got) != 3 {
		t.Fatalf("attachments %d, want the two that fit plus the reason", len(got))
	}
	for i, want := range []byte{'a', 'b'} {
		if got[i].MimeType != "image/jpeg" || got[i].Filename != []string{"1.jpg", "2.jpg"}[i] {
			t.Fatalf("attachment %d is %+v", i, got[i])
		}
		if len(got[i].Data) != partBytes || got[i].Data[0] != want || got[i].Err != nil || !got[i].Usable() {
			t.Fatalf("attachment %d is %d bytes, first %q: %+v", i, len(got[i].Data), got[i].Data[:1], got[i])
		}
	}
	// The part that did not fit is not handed over cut in half: a picture
	// truncated by the cap would fail normalization and cost the caller a
	// stored-then-skipped part without ever naming the bound that stopped it.
	stopped := got[2]
	if stopped.Usable() || len(stopped.Data) != 0 {
		t.Fatalf("the part that did not fit was decoded: %+v", stopped)
	}
	assertCapReason(t, stopped.Err, "Die Nachricht hat mehr Bilddaten, als diese Anwendung liest")
}

// The same bound for a message of many small parts, which the byte cap does not
// catch at all: they fit in the sum and cost a list entry each. The list stops
// at the part cap and the reason says which of the two bounds it was.
func TestIMAPPartCapStopsDecoding(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	const parts = 200
	pictures := make([]testPicture, 0, parts)
	for i := 0; i < parts; i++ {
		pictures = append(pictures, testPicture{
			Data:     pictureBytes(16, byte('a'+i%26)),
			MimeType: "image/jpeg",
			Filename: fmt.Sprintf("%d.jpg", i),
		})
	}
	ms.appendMessage(t, "INBOX", messageWithPictures("kasse@example.de", "Zweihundert Belege", pictures, nil))

	src := ms.source(t, Options{MaxParts: 3})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages %+v", messages)
	}
	got := messages[0].Attachments
	if len(got) != 4 {
		t.Fatalf("attachments %d, want three decoded parts plus the reason", len(got))
	}
	for i, att := range got[:3] {
		if len(att.Data) != 16 || att.Err != nil || !att.Usable() {
			t.Fatalf("attachment %d: %+v", i, att)
		}
	}
	stopped := got[3]
	if stopped.Usable() || len(stopped.Data) != 0 {
		t.Fatalf("the part behind the cap was decoded: %+v", stopped)
	}
	assertCapReason(t, stopped.Err, "Die Nachricht hat mehr Anhänge, als diese Anwendung liest")
}

// A message that stays inside both bounds is untouched, and a part over the
// per-part cap still reads cap+1 bytes with no reference to the whole-message
// bounds — the two are separate limits, and one of them firing says nothing
// about the other.
func TestIMAPMessageWithinBothCapsIsUntouched(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	pictures := []testPicture{
		{Data: pngBytes(), MimeType: "image/png", Filename: "1.png"},
		{Data: pngBytes(), MimeType: "image/png", Filename: "2.png"},
		{Data: []byte("%PDF-1.7"), MimeType: "application/pdf", Filename: "3.pdf"},
	}
	ms.appendMessage(t, "INBOX", messageWithPictures("kasse@example.de", "Zwei Belege", pictures, nil))

	src := ms.source(t, Options{MaxAttachmentBytes: 1 << 20, MaxMessageBytes: 1 << 20, MaxParts: 8})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages %+v", messages)
	}
	got := messages[0].Attachments
	if len(got) != 3 {
		t.Fatalf("attachments %d, want every part of the message", len(got))
	}
	for _, att := range got[:2] {
		if !bytes.Equal(att.Data, pngBytes()) || att.Err != nil {
			t.Fatalf("attachment %+v", att)
		}
	}
	if got[2].MimeType != "application/pdf" || got[2].Usable() {
		t.Fatalf("the PDF was read: %+v", got[2])
	}
}

// The sum is enforced while the part is read, not against the finished list —
// a cap the caller checked afterwards has already paid for the bytes it refuses.
// The counting reader below is what tells the two apart: it records how much of
// the body was actually pulled, so a read that stopped at the room proves the
// bound was in the reader and not in the caller.
func TestReadAttachmentStopsAtTheRoomLeft(t *testing.T) {
	raw := messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil)
	header := attachmentHeader(t, raw)

	t.Run("stops one byte past the room", func(t *testing.T) {
		body := &countingReader{Reader: bytes.NewReader(pngBytes())}
		att, read := readAttachment(header, body, 1<<20, 10)
		if len(body.seen) != 11 {
			t.Fatalf("pulled %d bytes out of a body with 10 bytes of room", len(body.seen))
		}
		if read != 11 {
			t.Fatalf("read %d, want the room plus the one byte that proves the part was over it", read)
		}
		// The caller is the one that decides what to do with a part that did
		// not fit; readBody drops it and stops rather than storing half a JPEG.
		if len(att.Data) != 11 {
			t.Fatalf("decoded %d bytes", len(att.Data))
		}
	})

	t.Run("an exhausted room reads one byte and stops", func(t *testing.T) {
		body := &countingReader{Reader: bytes.NewReader(pngBytes())}
		_, read := readAttachment(header, body, 1<<20, 0)
		if len(body.seen) != 1 || read != 1 {
			t.Fatalf("pulled %d bytes and reported %d, want one of each", len(body.seen), read)
		}
	})

	t.Run("the per-part cap still applies on its own", func(t *testing.T) {
		body := &countingReader{Reader: bytes.NewReader(pngBytes())}
		_, read := readAttachment(header, body, 10, 1<<20)
		if len(body.seen) != 11 || read != 11 {
			t.Fatalf("pulled %d bytes and reported %d, want cap+1", len(body.seen), read)
		}
	})
}

// countingReader remembers how much of a body was actually pulled out of it.
type countingReader struct {
	*bytes.Reader
	seen []byte
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.seen = append(r.seen, p[:n]...)
	return n, err
}

// assertCapReason is what a caller can learn from the part that stopped the
// decode: a fixed German sentence, no sender text, no server response, and a
// retry class that says the same bytes refuse the same way next time.
func assertCapReason(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("the bound that stopped the decode was not reported")
	}
	if err.Error() != want {
		t.Errorf("reason %q, want %q", err.Error(), want)
	}
	var call *Error
	if !asError(err, &call) {
		t.Fatalf("reason %v carries no retry class", err)
	}
	if call.RetryKind() != KindPermanent {
		t.Errorf("retry kind %q, want %q: these bytes refuse the same way on every retry", call.RetryKind(), KindPermanent)
	}
	assertNoLeak(t, "the reason", err.Error())
	if !strings.Contains(err.Error(), "Anwendung") {
		t.Errorf("reason %q does not say which bound stopped the decode", err.Error())
	}
}

// classify is the contract the job queue reads, so every IMAP status response
// maps onto a retry class on purpose.
func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"auth failed", &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAuthenticationFailed}, KindAuth},
		{"bad", &imap.Error{Type: imap.StatusResponseTypeBad}, KindPermanent},
		{"no", &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Mailbox in use"}, KindTransient},
		{"timeout", context.DeadlineExceeded, KindTransient},
		{"already classified", &Error{Kind: KindAuth, Text: "x"}, KindAuth},
		{"plain", fmt.Errorf("boom"), KindTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryKind(classify(tt.err)); got != tt.want {
				t.Fatalf("retry kind %q, want %q", got, tt.want)
			}
		})
	}
	if classify(nil) != nil {
		t.Fatal("a nil error must stay nil")
	}
}

// The printed form of a mailbox failure is what the job row and the log get, so
// it must name neither the mailbox nor anything the server sent back. The
// retry class is the part an operator and the queue actually need, and it is
// asserted here on the class rather than on the text.
func TestClassifyRedactsHostAndServerText(t *testing.T) {
	const host = "imap.example.de"
	const secret = "app-password"
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			// What a dial failure actually looks like: net.OpError names the
			// address it could not reach.
			name: "connection failure",
			err: &net.OpError{
				Op: "dial", Net: "tcp",
				Addr: &net.TCPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 993},
				Err:  &net.DNSError{Err: "i/o timeout", Name: host},
			},
			want: KindTransient,
		},
		{
			// The login is refused; the server explains itself in Text.
			name: "auth failure",
			err:  &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAuthenticationFailed, Text: "[" + secret + "] Invalid credentials"},
			want: KindAuth,
		},
	}
	needles := []string{host, "203.0.113.9", ":993", secret, "Invalid credentials"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.err)
			var call *Error
			if !asError(got, &call) {
				t.Fatalf("error %v carries no retry class", got)
			}
			if call.RetryKind() != tt.want {
				t.Fatalf("retry kind %q, want %q", call.RetryKind(), tt.want)
			}
			text := got.Error()
			for _, needle := range needles {
				if strings.Contains(text, needle) {
					t.Errorf("error text carries %q: %s", needle, text)
				}
			}
			if text == "" {
				t.Error("the error text is empty; the operator learns nothing")
			}
		})
	}
	// The cause stays reachable for errors.Is and errors.As, so the wrapping is
	// a redaction of the printed form rather than a loss of the failure.
	wrapped := classify(context.DeadlineExceeded)
	if !errors.Is(wrapped, context.DeadlineExceeded) {
		t.Errorf("the cause is no longer reachable: %v", wrapped)
	}
}

// The needles every redacted mailbox failure must not carry: the host and port
// the connection was made to, the address it came from, the password, and the
// server's own words.
var leakNeedles = []string{
	"imap.example.de", "203.0.113.9", ":993", "app-password",
	"tk_geheim", "Cannot store", "PRIVACYREQUIRED", "mailbox is locked",
}

// The STORE path is the one the redaction was first added for and then missed
// twice: probeKeyword classifies, the two warns next to sess.store did not.
// A server that answers BAD says plenty in that response — the account, the
// connection, sometimes the flag — and *imap.Error.Error() prints it verbatim.
// The three sites on the path are asserted together here, because the failure
// mode is a site somebody adds and forgets.
func TestIMAPStoreFailureLogsNoServerText(t *testing.T) {
	const password = "app-password"
	tests := []struct {
		name string
		wrap func(imapserver.Session) imapserver.Session
		// fails reports whether MarkIngested is expected to report a failure.
		// With a server that searches the keyword and refuses to store it,
		// neither marker lands and the poll has to say so.
		fails bool
	}{
		{
			// The probe said the keyword works and the store contradicts it:
			// the warn right before the \Seen fallback.
			name:  "searches the keyword, refuses to store it",
			wrap:  leakyKeywordStoreOnly,
			fails: true,
		},
		{
			// The probe already said the server keeps no keyword. The store
			// still carries one, fails, and warns on the other branch.
			name:  "keeps no keyword at all",
			wrap:  leakyServerWithoutKeywords,
			fails: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ms := newMemServerWith(t, "belege", password, nil, tt.wrap)
			ms.createMailbox(t, "INBOX")
			uid := ms.appendMessage(t, "INBOX", messageWithImage(
				"kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil))

			var buf bytes.Buffer
			src := ms.source(t, Options{Password: password, Log: slog.New(slog.NewJSONHandler(&buf, nil))})
			messages, err := src.Fetch(ctx, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 {
				t.Fatalf("fetched %d messages", len(messages))
			}

			err = src.MarkIngested(ctx, []imap.UID{uid})
			if tt.fails {
				var call *Error
				if !asError(err, &call) {
					t.Fatalf("error %v carries no retry class", err)
				}
				if call.RetryKind() != KindTransient {
					t.Fatalf("retry kind %q, want %q", call.RetryKind(), KindTransient)
				}
			} else if err != nil {
				t.Fatalf("the \\Seen fallback has to carry the poll: %v", err)
			}
			assertNoLeak(t, "the log", buf.String())
			if err != nil {
				assertNoLeak(t, "the error", err.Error())
			}
			// The operator still learns that something happened: the warnings are
			// the only place this shows up at all.
			if !strings.Contains(buf.String(), "mail server") {
				t.Errorf("the failed store was not reported at all: %s", buf.String())
			}
		})
	}
}

// leakyKeywordStoreOnly searches the keyword happily and refuses to store one,
// the way an inconsistent server behaves, and says far too much when it does.
func leakyKeywordStoreOnly(session imapserver.Session) imapserver.Session {
	return leakyStore{Session: session, searchKeyword: true}
}

// leakyServerWithoutKeywords also refuses to search on a keyword, which is the
// branch where the poll warns after the store rather than before the fallback.
func leakyServerWithoutKeywords(session imapserver.Session) imapserver.Session {
	return leakyStore{Session: session, searchKeyword: false}
}

type leakyStore struct {
	imapserver.Session
	searchKeyword bool
}

func (s leakyStore) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	if !s.searchKeyword {
		for _, flag := range append(append([]imap.Flag{}, criteria.Flag...), criteria.NotFlag...) {
			if !strings.HasPrefix(string(flag), `\`) {
				return nil, &imap.Error{
					Type: imap.StatusResponseTypeBad,
					Text: "Unsupported search key FLAG for mailbox is locked at imap.example.de:993",
				}
			}
		}
	}
	return s.Session.Search(kind, criteria, options)
}

func (s leakyStore) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	for _, flag := range flags.Flags {
		if !strings.HasPrefix(string(flag), `\`) {
			return &imap.Error{
				Type: imap.StatusResponseTypeBad,
				Code: "PRIVACYREQUIRED",
				Text: fmt.Sprintf("Cannot store %s: mailbox is locked at imap.example.de:993 from 203.0.113.9 (tk_geheim)", flag),
			}
		}
	}
	return s.Session.Store(w, numSet, flags, options)
}

// The same guarantee for the other path that reaches a warning with a raw
// error: a message this poll could not read. The parser quotes the offending
// header line back, and the header line is attacker-controlled mail content, so
// the warning has to be the fixed sentence instead.
func TestIMAPSkippedMessageLogsNoMailBody(t *testing.T) {
	ctx := context.Background()
	ms := newMemServer(t, "belege", "app-password", nil)
	ms.createMailbox(t, "INBOX")
	// The second line has no colon, so ReadHeader refuses the whole message and
	// names the line in its complaint. Everything a leak would carry is on it.
	ms.appendMessage(t, "INBOX", []byte(
		"From: kasse@example.de\r\n"+
			"betreff imap.example.de:993 app-password tk_geheim 203.0.113.9\r\n"+
			"Subject: Bon\r\n\r\n"+
			"Beleg im Anhang.\r\n"))

	var buf bytes.Buffer
	src := ms.source(t, Options{Log: slog.New(slog.NewJSONHandler(&buf, nil))})
	messages, err := src.Fetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("an unreadable message was ingested: %+v", messages)
	}
	assertNoLeak(t, "the log", buf.String())
	if !strings.Contains(buf.String(), "mail message skipped") {
		t.Errorf("the skipped message was not reported: %s", buf.String())
	}
}

// A broken attachment read reaches the log through internal/service/mailbox.go,
// so Attachment.Err is printed there and has to be redacted at the source: a
// read that died inside the literal reports the connection it died on.
func TestReadAttachmentRedactsTheTransferFailure(t *testing.T) {
	raw := messageWithImage("kasse@example.de", "Bon", pngBytes(), "image/png", "b.png", nil)
	header := attachmentHeader(t, raw)

	att, _ := readAttachment(header, failedTransfer{}, 1<<20, 1<<20)
	if att.Err == nil {
		t.Fatal("a transfer that failed has to be reported")
	}
	assertNoLeak(t, "the attachment error", att.Err.Error())
	// The cause stays reachable, so the redaction costs no diagnosis.
	var opErr *net.OpError
	if !errors.As(att.Err, &opErr) {
		t.Errorf("the cause is no longer reachable: %v", att.Err)
	}
}

// attachmentHeader pulls the one attachment out of a multipart message.
func attachmentHeader(t *testing.T, raw []byte) *mail.AttachmentHeader {
	t.Helper()
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			t.Fatal("the message has no attachment")
		}
		if err != nil {
			t.Fatal(err)
		}
		if header, ok := part.Header.(*mail.AttachmentHeader); ok {
			return header
		}
	}
}

// failedTransfer is what the connection does halfway through a literal.
type failedTransfer struct{}

func (failedTransfer) Read([]byte) (int, error) {
	return 0, &net.OpError{
		Op: "read", Net: "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 993},
		Err:  fmt.Errorf("use of closed network connection at imap.example.de:993"),
	}
}

func assertNoLeak(t *testing.T, what, text string) {
	t.Helper()
	for _, needle := range leakNeedles {
		if strings.Contains(text, needle) {
			t.Errorf("%s carries %q: %s", what, needle, text)
		}
	}
}

func TestNewWithoutHostIsNil(t *testing.T) {
	if New(Options{Username: "belege"}) != nil {
		t.Fatal("a source without a host must be nil")
	}
	if New(Options{Host: "imap.example.de"}) != nil {
		t.Fatal("a source without a user must be nil")
	}
	if src := New(Options{Host: "imap.example.de", Username: "belege"}); src == nil {
		t.Fatal("a complete source must not be nil")
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	src := New(Options{Host: "imap.example.de", Username: "belege"})
	if src.Address() != "imap.example.de:993" {
		t.Fatalf("address %s", src.Address())
	}
	if src.options.Inbox != "INBOX" || src.options.MaxAttachmentBytes != 15<<20 || src.options.Timeout <= 0 {
		t.Fatalf("defaults %+v", src.options)
	}
	if src.options.MaxMessageBytes != defaultMaxMessageBytes || src.options.MaxParts != defaultMaxParts {
		t.Fatalf("decode bounds %+v", src.options)
	}
	// The total is a bound on the sum and the per-part cap is a bound on one
	// part, so the total is never under it: an operator who allows a 100 MB
	// picture has to be able to receive one inside a mail. Raising the
	// per-part cap raises the total with it rather than dropping the message
	// that the picture arrives in.
	big := New(Options{Host: "imap.example.de", Username: "belege", MaxAttachmentBytes: 100 << 20})
	if big.options.MaxMessageBytes < big.options.MaxAttachmentBytes {
		t.Fatalf("total %d under the per-part cap %d", big.options.MaxMessageBytes, big.options.MaxAttachmentBytes)
	}
	if big.options.MaxMessageBytes != 100<<20 {
		t.Fatalf("total %d, want the per-part cap itself", big.options.MaxMessageBytes)
	}
	if src.DoneFolder() != "" {
		t.Fatalf("done folder %q", src.DoneFolder())
	}
}

// asError is errors.As with a shorter call site in the table tests.
func asError(err error, target **Error) bool {
	return errors.As(err, target)
}

// observer is a second, independent client used only by the tests to read the
// server state back the way an operator's mail client would.
func (ms *memServer) observer(t *testing.T) *imapclient.Client {
	t.Helper()
	client, err := ms.ln.dialer("", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Login("belege", "app-password").Wait(); err != nil {
		t.Fatal(err)
	}
	return client
}

// flags reads the stored flags of one message over the wire.
func (ms *memServer) flags(t *testing.T, mailbox string, uid imap.UID) map[imap.Flag]bool {
	t.Helper()
	client := ms.observer(t)
	if _, err := client.Select(mailbox, nil).Wait(); err != nil {
		t.Fatal(err)
	}
	messages, err := client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{Flags: true}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("fetched %d messages", len(messages))
	}
	out := make(map[imap.Flag]bool, len(messages[0].Flags))
	for _, flag := range messages[0].Flags {
		out[flag] = true
	}
	return out
}

// messageCount is the STATUS number the operator would see.
func (ms *memServer) messageCount(t *testing.T, mailbox string) int {
	t.Helper()
	data, err := ms.observer(t).Status(mailbox, &imap.StatusOptions{NumMessages: true}).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if data.NumMessages == nil {
		t.Fatal("no message count")
	}
	return int(*data.NumMessages)
}

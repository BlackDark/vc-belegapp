package mailinbox

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"mime"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

// Marker is the keyword this application sets once it has ingested a message.
// It is the authoritative "already done" marker. \Seen is set alongside it, but
// a phone mail client marks a receipt read the moment it previews the picture,
// and that must not hide it from the poll — with a keyword in place, a message
// stays in the queue until this application has taken its attachments. A server
// without RFC 5258 keywords rejects the flag; the code then falls back to \Seen
// alone, which every IMAP server has had since RFC 3501. A server that searches
// on the keyword but refuses to store it is downgraded to \Seen as well, and
// MarkIngested reports that.
const Marker = imap.Flag("$Belegapp")

// Options configures the IMAP source. Password is the plaintext secret; the
// caller reads it from the environment or from a mounted secret file.
type Options struct {
	Host     string
	Port     int
	Username string
	Password string
	// TLS selects implicit TLS (the default, normally port 993). STARTTLS
	// upgrades a plain connection (normally port 143). Exactly one of the two
	// has to be true: this application has no plaintext mode, so the password
	// never crosses the network in the clear.
	TLS      bool
	STARTTLS bool
	// Inbox is the polled folder, DoneFolder the folder an ingested message is
	// moved to. An empty DoneFolder only flags the message.
	Inbox      string
	DoneFolder string
	// MaxAttachmentBytes caps one decoded attachment. The source reads at most
	// one byte more than the cap so the caller can tell "at the cap" from
	// "over the cap" without this package having to buffer a hostile part.
	MaxAttachmentBytes int64
	Timeout            time.Duration
	Log                *slog.Logger
}

// IMAP is a Source backed by a real IMAP server.
//
// Every method dials its own short-lived connection and closes it again. A poll
// is a batch job that runs once per BELEGAPP_MAIL_INTERVAL, so three logins per
// poll cost less than the reconnect bookkeeping a pooled connection needs, and
// a server that hung up mid-poll cannot poison the next one. go-imap bounds
// every command itself (30 s for a response, 5 min for a literal), so a silent
// server cannot hold a worker forever. The mutex serialises the calls so two
// overlapping polls never interleave commands; the client is safe for concurrent
// use on its own, but the keyword capability below is per-connection state.
type IMAP struct {
	options Options
	address string

	mu   sync.Mutex
	dial Dialer
	// keywordUnusable records that this server accepted a search on $Belegapp
	// but refused to store it (see MarkIngested). It is sticky, because the
	// probe asks the wrong question: a store that fails does not fail again in
	// a way the probe could see. Guarded by mu like the dialer and only ever
	// touched inside with().
	keywordUnusable bool
}

// Dialer opens one connection. It exists so the test can hand the client the
// two ends of an in-process pipe instead of a socket.
type Dialer func(addr string, options *imapclient.Options) (*imapclient.Client, error)

// New builds an IMAP source. A missing host or user returns nil, which is how
// the application expresses "the operator did not configure a mailbox".
func New(options Options) *IMAP {
	host := strings.TrimSpace(options.Host)
	if host == "" || strings.TrimSpace(options.Username) == "" {
		return nil
	}
	if options.Port <= 0 {
		options.Port = 993
	}
	if strings.TrimSpace(options.Inbox) == "" {
		options.Inbox = "INBOX"
	}
	if options.MaxAttachmentBytes <= 0 {
		options.MaxAttachmentBytes = 15 << 20
	}
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	src := &IMAP{options: options, address: net.JoinHostPort(host, strconv.Itoa(options.Port))}
	src.dial = src.dialTLS
	return src
}

// Address is host:port, for the startup log.
func (s *IMAP) Address() string { return s.address }

// DoneFolder is where ingested messages are moved; empty means only flagging.
func (s *IMAP) DoneFolder() string { return s.options.DoneFolder }

func (s *IMAP) dialTLS(addr string, options *imapclient.Options) (*imapclient.Client, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	options.TLSConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if s.options.STARTTLS {
		return imapclient.DialStartTLS(addr, options)
	}
	return imapclient.DialTLS(addr, options)
}

// session is one connection: logged in, polled mailbox selected, keyword
// capability probed.
type session struct {
	src    *IMAP
	client *imapclient.Client
	// keyword reports whether the server stores the $Belegapp keyword. It
	// decides both the search criteria and the flags MarkIngested writes.
	keyword bool
}

// with opens a connection, runs fn, and closes it again whatever fn returns.
func (s *IMAP) with(ctx context.Context, fn func(*session) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	options := &imapclient.Options{Dialer: &net.Dialer{Timeout: s.options.Timeout}}
	client, err := s.dial(s.address, options)
	if err != nil {
		return classify(err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Login(s.options.Username, s.options.Password).Wait(); err != nil {
		return classify(err)
	}
	if _, err := client.Select(s.options.Inbox, nil).Wait(); err != nil {
		return classify(err)
	}
	sess := &session{src: s, client: client}
	sess.probeKeyword()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(sess)
}

// probeKeyword learns whether the server accepts the $Belegapp keyword. A
// server without keywords answers BAD to a search on one, and from then on the
// poll falls back to \Seen. Anything else is a real failure and stays one.
//
// A store that already refused the keyword outranks the probe: this process has
// seen the marker fail where the probe says it works, and the marker is what
// the poll searches on. Asking again would only get the answer the failed store
// already gave.
func (s *session) probeKeyword() {
	if s.src.keywordUnusable {
		s.keyword = false
		return
	}
	_, err := s.client.UIDSearch(&imap.SearchCriteria{Flag: []imap.Flag{Marker}}, nil).Wait()
	if err == nil {
		s.keyword = true
		return
	}
	// classify first: the raw error is an *imap.Error and carries the server
	// response verbatim, which warn promises never to log. The retry class it
	// picks is the same one a plain caller reads, because classify maps an
	// unrecognised server answer to transient — what retryKind returns for the
	// raw error too.
	failure := classify(err)
	switch retryKind(failure) {
	case KindPermanent, KindAuth:
		s.src.warn("mail server keeps no keyword flag; the poll falls back to \\Seen", "err", failure)
	default:
		s.src.warn("mail server does not answer the keyword probe", "err", failure)
	}
}

// criteria are the messages this poll wants: everything the server did not mark
// with our keyword. With a server that has no keywords that is \Seen, which is
// also the human-visible disposition, so the two agree.
func (s *session) criteria() *imap.SearchCriteria {
	if s.keyword {
		return &imap.SearchCriteria{NotFlag: []imap.Flag{Marker}}
	}
	return &imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}
}

// Fetch returns the first limit messages this poll has not ingested yet,
// oldest first.
func (s *IMAP) Fetch(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 1
	}
	var out []Message
	err := s.with(ctx, func(sess *session) error {
		data, err := sess.client.UIDSearch(sess.criteria(), &imap.SearchOptions{ReturnAll: true}).Wait()
		if err != nil {
			return classify(err)
		}
		uids, ok := data.All.(imap.UIDSet)
		if !ok {
			return nil
		}
		nums, ok := uids.Nums()
		if !ok || len(nums) == 0 {
			return nil
		}
		if len(nums) > limit {
			nums = nums[:limit]
		}
		messages, err := sess.read(nums)
		out = messages
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// read fetches the full body of each UID. A message that cannot be parsed is
// dropped from the batch and logged: it keeps its place in the mailbox and the
// next poll tries it again, which beats failing the whole poll on one malformed
// mail.
//
// The body section is a PEEK, and that is a product decision rather than a
// detail. RFC 3501 §6.4.8: a plain BODY fetch marks the message \Seen as a side
// effect, and a message whose sender the allowlist rejects has to keep every
// flag the operator gave it — docs/configuration.md promises exactly that. Only
// MarkIngested may set \Seen, on the messages the poll has taken.
func (s *session) read(uids []imap.UID) ([]Message, error) {
	section := &imap.FetchItemBodySection{Peek: true}
	options := &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{section},
	}
	cmd := s.client.Fetch(imap.UIDSetNum(uids...), options)
	defer func() { _ = cmd.Close() }()

	out := make([]Message, 0, len(uids))
	for {
		msg := cmd.Next()
		if msg == nil {
			break
		}
		parsed, err := s.parseMessage(msg)
		if err != nil {
			// classify first, for the same reason as probeKeyword: the MIME
			// parser quotes the line of the mail body that tripped it, and a
			// literal reader that dies mid-transfer reports a net.OpError
			// naming host and port. parseMessage itself already answers with a
			// fixed German sentence, so this only makes the guarantee hold at
			// the log site itself rather than at the producer.
			s.src.warn("mail message skipped", "err", classify(err))
			continue
		}
		out = append(out, parsed)
	}
	return out, classify(cmd.Close())
}

// parseMessage reads the sender and the attachments out of one FETCH response.
// The subject is read for the log line only; it is never stored and never sent
// anywhere but the local log.
//
// Every error it returns is an *Error, which means the printed form is a fixed
// German sentence and never anything the mail or the server sent: the MIME
// parser quotes the offending line back, and a literal that dies mid-transfer
// reports a net.OpError with host and port in it. The cause stays reachable
// through Unwrap for errors.Is and errors.As.
//
// The body section arrives as a live stream that is only valid until the next
// item is pulled, so the message is parsed inside the item loop rather than
// after it. That also keeps a large attachment out of memory twice: the parts
// are decoded one at a time and each one is capped.
func (s *session) parseMessage(msg *imapclient.FetchMessageData) (Message, error) {
	out := Message{}
	seen := false
	for {
		item := msg.Next()
		if item == nil {
			break
		}
		switch item := item.(type) {
		case imapclient.FetchItemDataUID:
			out.UID = item.UID
		case imapclient.FetchItemDataBodySection:
			if err := readBody(&out, item.Literal, s.src.options.MaxAttachmentBytes); err != nil {
				return out, err
			}
			seen = true
		}
	}
	if out.UID == 0 {
		return out, transient("Der Abruf lieferte keine Nachrichtennummer", nil)
	}
	if !seen {
		return out, transient("Der Abruf lieferte keinen Nachrichtentext", nil)
	}
	if out.From == "" {
		return out, transient("Die Nachricht hat keinen Absender", nil)
	}
	return out, nil
}

// readBody fills the sender and the attachments from one message body. Every
// error it returns is an *Error for the reason parseMessage gives.
func readBody(out *Message, literal io.Reader, maxBytes int64) error {
	reader, err := mail.CreateReader(literal)
	if err != nil {
		return transient("Der Nachrichtentext ist nicht lesbar", err)
	}
	from, err := reader.Header.AddressList("From")
	if err != nil || len(from) == 0 {
		return transient("Die Nachricht hat keinen lesbaren Absender", err)
	}
	out.From = from[0].Address
	if subject, err := reader.Header.Text("Subject"); err == nil {
		out.Subject = subject
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return transient("Ein Nachrichtenteil ist nicht lesbar", err)
		}
		header, ok := part.Header.(*mail.AttachmentHeader)
		if !ok {
			// The text body is not a receipt. Skip it without reading.
			continue
		}
		out.Attachments = append(out.Attachments, readAttachment(header, part.Body, maxBytes))
	}
}

// readAttachment decodes one part.
//
// The declared media type is reported either way, so the caller can log that a
// PDF arrived and skip it, but the bytes are only read for the three picture
// types this application stores. Reading a 90 MB PDF just to throw it away
// would put the memory limit of SPEC NF-3 at risk for nothing; the declared
// type is enough to reject it.
//
// This is where the product decision lives: MimeType carries the canonical
// media type and Data is nil for anything the caller will not read. The caller
// applies its own allowlist — see StorableImageType.
func readAttachment(header *mail.AttachmentHeader, body io.Reader, limit int64) Attachment {
	declared, _, err := header.ContentType()
	if err != nil {
		// A part without a Content-Type is inline text, not an attachment.
		return Attachment{}
	}
	name, err := header.Filename()
	if err != nil {
		name = ""
	}
	out := Attachment{Filename: name, MimeType: canonicalMediaType(declared)}
	if !StorableImageType(out.MimeType) {
		return out
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		// The same rule as every other failure on this path: the caller logs
		// this one (internal/service/mailbox.go), and a read that died inside
		// the literal reports the connection it died on.
		out.Err = transient("Der Anhang ist nicht lesbar", err)
		return out
	}
	out.Data = data
	return out
}

// readableTypes are the media types whose bytes are worth reading. It is a
// transport-level guard, not the product policy: the service decides what it
// stores, and it also has to reject what this map already skipped. Both sides
// ask StorableImageType, so there is one list of picture types in the intake.
var readableTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// StorableImageType reports whether a declared media type is a picture this
// intake decodes and stores. The source uses it to decide which bodies are
// worth reading at all; the service uses it as the policy check, because a
// Source is an interface and another implementation may hand it anything.
func StorableImageType(mimeType string) bool {
	return readableTypes[strings.ToLower(strings.TrimSpace(mimeType))]
}

// canonicalMediaType lowercases the type and drops its parameters, and maps the
// handful of aliases mail clients emit onto the spelling the database stores.
func canonicalMediaType(raw string) string {
	mimeType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg", "image/pjpeg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	case "image/webp":
		return "image/webp"
	default:
		return strings.ToLower(mimeType)
	}
}

// MarkIngested sets \Seen and the keyword. When the server refuses the keyword
// the \Seen store is still sent, so the message leaves the queue either way.
// \Deleted is never set and nothing is expunged.
//
// Which of the three failures below is which decides what the caller learns:
//
//   - The probe said this server keeps no keyword. The \Seen store is then
//     exactly the marker the next poll searches on, so the fallback is the
//     normal path and its result is the result.
//   - An earlier store in this process proved the keyword unusable. The probe
//     skips the question from then on, so \Seen alone is the whole
//     disposition: asking for the keyword again would send a STORE the server
//     refuses and a warning to the operator on every single poll, for a marker
//     nothing reads any more.
//   - The probe said the keyword works and the store contradicts it. Neither
//     marker is persisted, so the next poll — which searches on the keyword
//     alone — hands the same messages back and the intake duplicates every
//     receipt once per interval. There is no silent recovery here, so this
//     process stops trusting the keyword, sets \Seen so the messages do leave
//     the queue, and returns a transient error: the job queue retries instead
//     of recording a success that did not happen.
func (s *IMAP) MarkIngested(ctx context.Context, uids []imap.UID) error {
	if len(uids) == 0 {
		return nil
	}
	return s.with(ctx, func(sess *session) error {
		if sess.src.keywordUnusable {
			// Known bad for the rest of this process, so criteria() searches
			// on \Seen alone and this store is the whole disposition.
			return classify(sess.store(uids, []imap.Flag{imap.FlagSeen}))
		}
		err := sess.store(uids, []imap.Flag{imap.FlagSeen, Marker})
		if err == nil {
			return nil
		}
		if sess.keyword {
			sess.src.keywordUnusable = true
			// classify first: this is an *imap.Error straight out of sess.store
			// and its Error() is "Type Code: Text", the server response
			// verbatim. The warn below promises that never reaches the log.
			sess.src.warn("mail server refuses to store the keyword flag although it searches on it; the poll falls back to \\Seen for the rest of this process", "err", classify(err))
			if fallback := sess.store(uids, []imap.Flag{imap.FlagSeen}); fallback != nil {
				return classify(fallback)
			}
			return transient("Der Mailserver weist das Keyword-Flag zurück; der Poll nutzt \\Seen", err)
		}
		// The server will not keep the keyword. Fall back to \Seen, which is
		// then also what the next poll searches on, so the message is not
		// re-ingested forever. classify first, as above: the raw error is the
		// server response.
		sess.src.warn("mail server refused the keyword flag; only \\Seen is set", "err", classify(err))
		return classify(sess.store(uids, []imap.Flag{imap.FlagSeen}))
	})
}

// Move relocates the messages into folder.
//
// The RFC 6851 MOVE command is the only way this package removes a message from
// a folder: it copies first and only then unlinks the source, so nothing is lost
// if the copy fails. go-imap falls back to COPY + STORE \Deleted + EXPUNGE when
// a server does not advertise MOVE, and that fallback is exactly what this
// application must never do, so the capability is checked here and a server
// without MOVE is reported as a permanent error instead.
func (s *IMAP) Move(ctx context.Context, uids []imap.UID, folder string) error {
	folder = strings.TrimSpace(folder)
	if len(uids) == 0 || folder == "" {
		return nil
	}
	return s.with(ctx, func(sess *session) error {
		if !sess.client.Caps().Has(imap.CapMove) {
			return permanent("Der Mailserver kann kein MOVE; BELEGAPP_MAIL_DONE_FOLDER bleibt unbesetzt", nil)
		}
		_, err := sess.client.Move(imap.UIDSetNum(uids...), folder).Wait()
		return classify(err)
	})
}

func (s *session) store(uids []imap.UID, flags []imap.Flag) error {
	silent := imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: flags}
	return s.client.Store(imap.UIDSetNum(uids...), &silent, nil).Close()
}

// warn logs an operator-facing finding. No password, no mail body, and no full
// server response ever reach the log.
func (s *IMAP) warn(msg string, args ...any) {
	if s.options.Log == nil {
		return
	}
	s.options.Log.Warn(msg, args...)
}

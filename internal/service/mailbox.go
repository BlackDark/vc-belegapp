package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/emersion/go-imap/v2"

	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/mailinbox"
	"github.com/BlackDark/vc-belegapp/internal/notify"
	"github.com/BlackDark/vc-belegapp/internal/problem"
)

// defaultMaxMessagesPerPoll bounds one poll when the operator set no value of
// their own. A mailbox that accumulated a thousand receipts while the process
// was down is drained over the next polls instead of in one job that would hold
// a worker for an hour. BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL is the dial, because
// "a backlog that large is worth several intervals of waiting" is true on a
// phone hotspot and wrong on a server line.
const defaultMaxMessagesPerPoll = 50

// MailPoll is the result of one IngestMailbox run.
type MailPoll struct {
	// Geprueft is how many messages the source returned.
	Geprueft int `json:"geprueft"`
	// Abgelehnt counts messages from a sender outside the allowlist. They are
	// left where they are and only reported in the log.
	Abgelehnt int `json:"abgelehnt"`
	// OhneBild counts messages with no attachment this application stores.
	OhneBild int `json:"ohne_bild"`
	// Bilder counts the Belegbilder that were written.
	Bilder int `json:"bilder"`
	// Uebersprungen counts attachments that were not stored: an unsupported
	// type, a broken transfer, an attachment over the per-message cap, or one
	// the storage path rejected. Every one of them is logged with its reason.
	Uebersprungen int `json:"uebersprungen"`
}

// IngestMailbox polls the configured mailbox once and stores every image
// attachment as an unassigned Belegbild for the *Offene Bilder* screen.
//
// The order inside one message is deliberate. Allowlist first, so a stranger
// never gets their bytes normalised and hashed. Then the attachments, each
// through SaveBild, which is the one path that already enforces the byte cap,
// the format list, and the recognition enqueue. Only then the disposition:
// \Seen plus the marker keyword, and a move when a done folder is configured.
//
// A message is consumed once the poll has looked at it. A single attachment
// that cannot be stored — a corrupt file, a picture over the pixel limit — is
// logged and skipped; it decides the same way on every retry, so leaving the
// message in the queue would re-read it forever and add one copy of every
// image it *did* carry on every interval.
//
// A failure of the store, the database or the network is a property of the
// process, not of the attachment, so it ends the poll at once. What happens to
// the interrupted message is decided by what already reached the disk, because
// that is the only thing a later poll cannot undo. With at least one image
// stored it is flagged before the error leaves — those images are committed,
// and re-reading the message would store them a second time — and its
// remaining attachments are counted as skipped, so the poll summary says what
// is missing instead of pretending the receipt was complete. With nothing
// stored there is nothing to duplicate, so it stays unmarked and the next poll
// starts it over.
//
// Flagging is itself a network call and it fails on its own account. The marker
// it writes is the only durable "already done" note, so a store the server
// refuses leaves the message in the queue — and re-reading it would store every
// picture behind it a second time. Those messages are therefore noted in this
// process as well (mailConsumed) and only offered to the marker until the
// server takes it; the ones that stored nothing are not noted, because a
// receipt the store never committed has to survive the outage.
//
// Mail is never deleted. This method does not set \Deleted and does not expunge.
func (s *Service) IngestMailbox(ctx context.Context) (MailPoll, error) {
	var out MailPoll
	if s.Mail == nil {
		return out, nil
	}
	messages, err := s.Mail.Fetch(ctx, s.mailPollLimit())
	if err != nil {
		return out, err
	}
	out.Geprueft = len(messages)

	seen := make([]imap.UID, 0, len(messages))
	// committed is the part of seen with images on disk behind it. It is the
	// only part a later poll may not read again, so it is what the note below
	// remembers when the marker fails.
	committed := make([]imap.UID, 0, len(messages))
	var failed error
	for _, msg := range messages {
		if err := ctx.Err(); err != nil {
			// Stop here. This message and the rest stay unmarked so the next
			// poll reads them; the ones before it are still flagged below.
			failed = err
			break
		}
		if s.unflagged.has(msg.UID) {
			// This process already stored this message and the server has not
			// taken the marker since. The attachments are still on the wire,
			// but SaveBild does not dedupe, so reading them again would put a
			// second copy of every picture behind the flag failure. The UID is
			// offered to the marker and nothing else, until the server accepts
			// it and the message leaves the queue for good.
			seen = append(seen, msg.UID)
			continue
		}
		if !s.senderAllowed(msg.From) {
			out.Abgelehnt++
			s.warnMail("mail from a sender outside the allowlist was ignored",
				"absender", msg.From, "uid", msg.UID)
			continue
		}

		stored, skipped, err := s.ingestAttachments(ctx, msg)
		out.Bilder += stored
		out.Uebersprungen += skipped
		if err != nil {
			// The store or the database is the problem, not this message, so
			// the poll ends here. What happens to this one message follows the
			// same rule as everywhere else: only what is already on disk counts.
			// With an image stored it is flagged before the error leaves,
			// because reading it again would store that image a second time,
			// and the attachments it could not take are counted as skipped
			// above. With nothing stored there is nothing to duplicate, so it
			// stays in the queue and the next poll reads it from its first
			// attachment — a store that is down for a whole hour costs no
			// receipts that way.
			if stored > 0 {
				seen = append(seen, msg.UID)
				committed = append(committed, msg.UID)
			}
			failed = err
			break
		}
		if stored == 0 {
			out.OhneBild++
			s.warnMail("mail carried no image this application stores",
				"uid", msg.UID, "betreff", msg.Subject)
		}
		seen = append(seen, msg.UID)
		if stored > 0 {
			committed = append(committed, msg.UID)
		}
	}

	// The disposition runs even for an aborted poll: every message in seen has
	// images on disk behind it, so the marker has to be written before the
	// error travels on. A failure here is logged next to the one that stopped
	// the poll rather than replacing it.
	if len(seen) > 0 {
		if err := s.Mail.MarkIngested(ctx, seen); err != nil {
			// The marker is the only note that survives this process, and this
			// call is the only thing that writes it. While it fails the messages
			// stay in the queue, so the images already on disk would be stored a
			// second time on the next poll. The messages in committed are noted
			// here instead and offered to the marker again on every poll until
			// the server accepts one. A message that stored nothing is not
			// noted: there is nothing behind it to duplicate, and the receipt
			// it still carries has to outlive the outage.
			s.unflagged.add(committed)
			if failed == nil {
				failed = err
			} else {
				s.warnMail("mail could not be flagged; this process keeps the images it already stored", "err", err)
			}
		} else {
			// The server has the flag now, so this process' note of it is out
			// of date and the note shrinks back to the messages the *next*
			// failed flag leaves behind.
			s.unflagged.forget(seen)
		}
	}
	if failed != nil {
		return out, failed
	}
	if len(seen) == 0 {
		return out, nil
	}
	folder := s.MailDoneFolder
	if folder == "" {
		return out, nil
	}
	if err := s.Mail.Move(ctx, seen, folder); err != nil {
		// The images are stored and the messages are flagged; only the filing
		// failed. A missing done folder is permanent and would otherwise retry
		// three times per poll forever, so it is reported but not fatal.
		s.warnMail("mail could not be filed in the done folder",
			"ordner", folder, "err", err)
	}
	return out, nil
}

// mailPollLimit is how many messages one poll takes.
func (s *Service) mailPollLimit() int {
	if s.MailMaxMessages > 0 {
		return s.MailMaxMessages
	}
	return defaultMaxMessagesPerPoll
}

// mailConsumed is this process' own note of the messages whose images are on
// disk and whose marker the server has not taken yet.
//
// The keyword on the server is the only record of that fact that outlives the
// poll, and MarkIngested is the only thing that writes it. A store the server
// refuses — one dropped packet on the IMAP STORE command is enough — therefore
// hands the very same messages back on the next poll, and SaveBild does not
// dedupe, so every picture behind it would reach the blob store a second time.
// What is remembered here is not a guess about the message but a fact about
// this run: those exact UIDs went through SaveBild and came out stored.
//
// Memory and not a column, which is a deliberate limit. The note holds one UID
// per message the server refused to flag and nothing else; every poll offers
// the remembered UIDs to MarkIngested again, so the first flag the server does
// accept empties it and it grows with the outage rather than with the mailbox.
// Restarting during a flag outage costs one duplicate of that one poll's
// batch — the marker, once written, is what closes the window for good.
//
// It is guarded because a poll runs on a job worker while the ticker that
// decides whether to enqueue one runs on another goroutine.
type mailConsumed struct {
	mu   sync.Mutex
	uids []imap.UID
	set  map[imap.UID]bool
}

// has reports whether this message is already stored here and still unflagged.
func (m *mailConsumed) has(uid imap.UID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.set[uid]
}

// add notes the messages whose images reached the disk behind a failed flag.
func (m *mailConsumed) add(uids []imap.UID) {
	if len(uids) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set == nil {
		m.set = make(map[imap.UID]bool, len(uids))
	}
	for _, uid := range uids {
		if m.set[uid] {
			continue
		}
		m.set[uid] = true
		m.uids = append(m.uids, uid)
	}
}

// forget drops the messages the server has flagged, so the note only ever holds
// what it still has to say. The slice keeps the order the messages were stored
// in, which is what the poll hands back to the marker.
func (m *mailConsumed) forget(uids []imap.UID) {
	if len(uids) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set == nil {
		return
	}
	for _, uid := range uids {
		delete(m.set, uid)
	}
	kept := m.uids[:0]
	for _, uid := range m.uids {
		if m.set[uid] {
			kept = append(kept, uid)
		}
	}
	m.uids = kept
}

// ingestAttachments stores one message's pictures. It returns how many became
// Belegbilder and how many the service did not keep.
//
// A content rejection from SaveBild (E_BILD_FORMAT, E_BILD_ZU_GROSS) is a
// property of the attachment, not of the intake: the same bytes reject the same
// way on every retry, so the part is logged and skipped and the message is
// still consumed. Everything else — the blob store, the database, a cancelled
// context — is returned, because that is a property of the process and the next
// poll has to try again.
//
// The skipped count that comes back with such an error covers this attachment
// and every one behind it: they were never stored, whatever the next poll
// decides to do with the message, and counting them is what keeps the poll
// summary from claiming a receipt it only read in part.
//
// SaveBild does not dedupe, so the caller may not read the same message twice.
// The disposition in IngestMailbox is what guarantees it: a message is flagged
// as soon as one of its images is on disk, and a message that reached the store
// error without having stored anything is left unmarked and read again from the
// first attachment, because there is nothing yet to duplicate. The flag is a
// network call like any other, so the note in mailConsumed backs it up for the
// case where the server refuses it.
func (s *Service) ingestAttachments(ctx context.Context, msg mailinbox.Message) (stored, skipped int, err error) {
	limit := s.MailMaxAttachments
	if limit < 1 {
		limit = 1
	}
	for i, attachment := range msg.Attachments {
		if attachment.Err != nil {
			// A broken transfer is not the message's fault; skip the part.
			skipped++
			s.warnMail("mail attachment could not be read", "uid", msg.UID, "datei", attachment.Filename, "err", attachment.Err)
			continue
		}
		if !mailinbox.StorableImageType(attachment.MimeType) {
			skipped++
			typ := attachment.MimeType
			if typ == "" {
				typ = "unbekannt"
			}
			s.warnMail("mail attachment skipped", "uid", msg.UID, "datei", attachment.Filename, "typ", typ)
			continue
		}
		if stored >= limit {
			skipped++
			s.warnMail("mail attachment skipped by the per-message cap",
				"uid", msg.UID, "datei", attachment.Filename, "grenze", limit)
			continue
		}
		if s.UploadMax > 0 && int64(len(attachment.Data)) > s.UploadMax {
			skipped++
			s.warnMail("mail attachment is over the upload limit",
				"uid", msg.UID, "datei", attachment.Filename, "grenze", s.UploadMax)
			continue
		}
		if _, err := s.SaveBildFrom(ctx, notify.SourceMail, attachment.Data, true); err != nil {
			if !isContentRejection(err) {
				// The store or the database is down. This attachment and the
				// ones behind it did not make it to disk, so they are counted
				// as skipped rather than dropped silently, and the caller is
				// told how many of this message's images are already stored.
				return stored, skipped + len(msg.Attachments) - i, err
			}
			skipped++
			s.warnMail("mail attachment rejected", "uid", msg.UID, "datei", attachment.Filename, "err", err)
			continue
		}
		stored++
	}
	return stored, skipped, nil
}

// isContentRejection reports whether SaveBild refused the bytes themselves. A
// client-visible problem is the answer for an upload: this intake is the client
// of its own storage path, so a 4xx is the attachment talking, not the server.
func isContentRejection(err error) bool {
	var p *problem.Error
	return errors.As(err, &p) && p.Status >= 400 && p.Status < 500
}

// senderAllowed applies BELEGAPP_MAIL_ALLOWED_SENDERS. An empty list admits
// every sender; the comparison is on the address alone, case-insensitively, and
// on an exact match rather than a suffix, so an allowlist entry can never match
// a lookalike domain.
func (s *Service) senderAllowed(from string) bool {
	if len(s.MailAllowedSenders) == 0 {
		return true
	}
	from = strings.ToLower(strings.TrimSpace(from))
	if from == "" {
		return false
	}
	for _, allowed := range s.MailAllowedSenders {
		if strings.ToLower(strings.TrimSpace(allowed)) == from {
			return true
		}
	}
	return false
}

// mailJobPayload is the constant body of a mail job. The poll takes no
// arguments, and a constant payload is what lets the ticker recognise its own
// open job and skip a tick rather than pile another one on top.
const mailJobPayload = "{}"

// PollMailbox enqueues a mail job every MailInterval until ctx ends.
//
// It does nothing at all when no mailbox is configured, so an instance without
// the feature never grows a queue row. The job queue owns the IMAP I/O and its
// retries; this loop only ticks, and it skips a tick while a mail job is still
// waiting or running — without that, an unreachable server would pile one
// extra job onto the backlog on every tick.
func (s *Service) PollMailbox(ctx context.Context) {
	if s.Mail == nil {
		return
	}
	s.pollLoop(ctx, s.MailInterval, s.enqueueMail)
}

// enqueueMail queues one poll unless one is already open.
func (s *Service) enqueueMail(ctx context.Context) {
	s.enqueueUnlessOpen(ctx, jobs.TypMail, mailJobPayload)
}

// runMail is the mail job body.
func (s *Service) runMail(ctx context.Context) (string, error) {
	poll, err := s.IngestMailbox(ctx)
	if err != nil {
		return "", err
	}
	if poll.Bilder > 0 {
		s.log("mail ingested", "bilder", poll.Bilder, "nachrichten", poll.Geprueft)
	}
	body, err := json.Marshal(poll)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// warnMail logs a finding the operator should look at: a rejected sender, a
// skipped attachment, a filing that did not happen. Never the password and
// never a mail body.
func (s *Service) warnMail(msg string, args ...any) {
	s.logWarn(msg, args...)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/BlackDark/vc-belegapp/internal/db"
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
	var failed error
	for _, msg := range messages {
		if err := ctx.Err(); err != nil {
			// Stop here. This message and the rest stay unmarked so the next
			// poll reads them; the ones before it are still flagged below.
			failed = err
			break
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
	}

	// The disposition runs even for an aborted poll: every message in seen has
	// images on disk behind it, so the marker has to be written before the
	// error travels on. A failure here is logged next to the one that stopped
	// the poll rather than replacing it.
	if len(seen) > 0 {
		if err := s.Mail.MarkIngested(ctx, seen); err != nil {
			if failed == nil {
				failed = err
			} else {
				s.warnMail("mail could not be flagged; the next poll reads it again", "err", err)
			}
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
// first attachment, because there is nothing yet to duplicate.
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
	if s.Mail == nil || s.Jobs == nil || s.MailInterval <= 0 {
		return
	}
	s.enqueueMail(ctx)
	ticker := time.NewTicker(s.MailInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.enqueueMail(ctx)
		}
	}
}

// enqueueMail queues one poll unless one is already open.
func (s *Service) enqueueMail(ctx context.Context) {
	n, err := db.New(s.DB.Write).CountOpenJobsByPayload(ctx, db.CountOpenJobsByPayloadParams{
		Typ:     "mail",
		Payload: mailJobPayload,
	})
	if err != nil {
		s.logMail("mail job could not be counted", "err", err)
		return
	}
	if n > 0 {
		return
	}
	if _, err := s.Jobs.Enqueue(ctx, "mail", mailJobPayload); err != nil {
		s.logMail("mail job could not be enqueued", "err", err)
	}
}

// runMail is the mail job body.
func (s *Service) runMail(ctx context.Context) (string, error) {
	poll, err := s.IngestMailbox(ctx)
	if err != nil {
		return "", err
	}
	if poll.Bilder > 0 {
		s.logMail("mail ingested", "bilder", poll.Bilder, "nachrichten", poll.Geprueft)
	}
	body, err := json.Marshal(poll)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *Service) logMail(msg string, args ...any) {
	if s.Log == nil {
		return
	}
	s.Log.Info(msg, args...)
}

// warnMail logs a finding the operator should look at: a rejected sender, a
// skipped attachment, a filing that did not happen. Never the password and
// never a mail body.
func (s *Service) warnMail(msg string, args ...any) {
	if s.Log == nil {
		return
	}
	s.Log.Warn(msg, args...)
}

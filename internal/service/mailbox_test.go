package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/mailinbox"
	"github.com/BlackDark/vc-belegapp/internal/recognition"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// mailFixture builds a Service with the IMAP intake switched on and a fake
// Source, on the shared dbtest database and clock.
func mailFixture(t *testing.T, box *mailinbox.Fake, senders ...string) *Service {
	t.Helper()
	svc := openService(t, recognition.Fake{})
	svc.Log = slog.New(slog.DiscardHandler)
	svc.Mail = box
	svc.MailInterval = testIntakeInterval
	svc.MailMaxAttachments = 3
	svc.MailAllowedSenders = senders
	return svc
}

func mailWith(uid imap.UID, from string, attachments ...mailinbox.Attachment) mailinbox.Message {
	return mailinbox.Message{UID: uid, From: from, Subject: "Beleg", Attachments: attachments}
}

func jpegAttachment(t *testing.T, r, g, b uint8) mailinbox.Attachment {
	t.Helper()
	return mailinbox.Attachment{Filename: "beleg.jpg", MimeType: "image/jpeg", Data: colorPNG(t, r, g, b)}
}

func countBilder(t *testing.T, svc *Service) int {
	t.Helper()
	var n int
	if err := svc.DB.Read.QueryRow(`SELECT COUNT(*) FROM belegbilder`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIngestMailboxStoresAttachmentsAsUnassigned(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 10, 20, 30), jpegAttachment(t, 40, 50, 60)),
	}}
	svc := mailFixture(t, box)

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Geprueft != 1 || poll.Bilder != 2 || poll.Abgelehnt != 0 || poll.OhneBild != 0 {
		t.Fatalf("poll %+v", poll)
	}
	// The images land unassigned, which is what makes them show up in the
	// *Offene Bilder* screen for review.
	list, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Gesamt != 2 || len(list.Bilder) != 2 {
		t.Fatalf("offene bilder %+v", list)
	}
}

// An empty allowlist admits every sender; the operator opted in to that.
func TestIngestMailboxEmptyAllowlistAcceptsEveryone(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "irgendwer@example.com", jpegAttachment(t, 1, 2, 3)),
	}}
	svc := mailFixture(t, box)

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 1 || poll.Abgelehnt != 0 {
		t.Fatalf("poll %+v", poll)
	}
}

func TestIngestMailboxAllowlistAcceptsAndRejects(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "Kasse@Example.de", jpegAttachment(t, 10, 10, 10)),
		mailWith(2, "kasse@example.de", jpegAttachment(t, 20, 20, 20)),
		mailWith(3, "kasse@example.de.evil.test", jpegAttachment(t, 30, 30, 30)),
		mailWith(4, "anders@example.de", jpegAttachment(t, 40, 40, 40)),
		mailWith(5, " Belege@example.de ", jpegAttachment(t, 50, 50, 50)),
	}}
	svc := mailFixture(t, box, "kasse@example.de", "belege@example.de")

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Geprueft != 5 {
		t.Fatalf("geprueft %d", poll.Geprueft)
	}
	// The two rejected senders: a lookalike domain is not a suffix match, and
	// a different address is simply not on the list. Case and surrounding
	// spaces are normalised on both sides.
	if poll.Abgelehnt != 2 {
		t.Fatalf("abgelehnt %d", poll.Abgelehnt)
	}
	if poll.Bilder != 3 {
		t.Fatalf("bilder %d", poll.Bilder)
	}
	// A rejected message is never flagged: it stays untouched for the operator
	// to inspect, and it is not consumed by the poll either.
	if len(box.Seen) != 3 {
		t.Fatalf("seen %v", box.Seen)
	}
	for _, uid := range box.Seen {
		if uid == 3 || uid == 4 {
			t.Fatalf("rejected message %d was marked", uid)
		}
	}
}

// SPEC §1.2 keeps PDF out of v1 and the database constrains the stored mime to
// image/jpeg, so a PDF attachment is skipped and logged, not converted.
func TestIngestMailboxSkipsPDF(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de",
			mailinbox.Attachment{Filename: "beleg.pdf", MimeType: "application/pdf", Data: []byte("%PDF-1.7")},
			jpegAttachment(t, 70, 70, 70),
		),
	}}
	svc := mailFixture(t, box, "kasse@example.de")

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 1 {
		t.Fatalf("bilder %d, want only the JPEG", poll.Bilder)
	}
	if countBilder(t, svc) != 1 {
		t.Fatalf("stored images %d", countBilder(t, svc))
	}
}

// A message with nothing this application stores is still consumed, otherwise
// the poll would re-read it every interval forever.
func TestIngestMailboxMessageWithoutUsableAttachment(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de",
			mailinbox.Attachment{Filename: "beleg.pdf", MimeType: "application/pdf", Data: []byte("%PDF")}),
		mailWith(2, "kasse@example.de",
			mailinbox.Attachment{Filename: "beleg.jpg", MimeType: "image/jpeg", Err: errors.New("connection reset")}),
	}}
	svc := mailFixture(t, box, "kasse@example.de")

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll.OhneBild != 2 || poll.Bilder != 0 {
		t.Fatalf("poll %+v", poll)
	}
	if len(box.Seen) != 2 {
		t.Fatalf("seen %v, want both consumed", box.Seen)
	}
}

func TestIngestMailboxPerMessageCap(t *testing.T) {
	ctx := context.Background()
	attachments := []mailinbox.Attachment{
		jpegAttachment(t, 10, 0, 0),
		jpegAttachment(t, 20, 0, 0),
		jpegAttachment(t, 30, 0, 0),
		jpegAttachment(t, 40, 0, 0),
		jpegAttachment(t, 50, 0, 0),
	}
	box := &mailinbox.Fake{Messages: []mailinbox.Message{mailWith(1, "kasse@example.de", attachments...)}}
	svc := mailFixture(t, box, "kasse@example.de")

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 3 {
		t.Fatalf("bilder %d, want the cap of 3", poll.Bilder)
	}
	if poll.Uebersprungen != 2 {
		t.Fatalf("uebersprungen %d", poll.Uebersprungen)
	}
	if countBilder(t, svc) != 3 {
		t.Fatalf("stored images %d", countBilder(t, svc))
	}
}

// An attachment over BELEGAPP_UPLOAD_MAX_BYTES is skipped, not truncated: a
// half JPEG would fail normalisation and waste a poll.
func TestIngestMailboxSkipsOversizeAttachment(t *testing.T) {
	ctx := context.Background()
	small := jpegAttachment(t, 9, 9, 9)
	big := jpegAttachment(t, 1, 2, 3)
	big.Data = append(big.Data, make([]byte, 4096)...)
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", big, small),
	}}
	svc := mailFixture(t, box, "kasse@example.de")
	// The cap sits exactly on the small attachment: at the limit is stored, one
	// byte over is not.
	svc.UploadMax = int64(len(small.Data))

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Bilder != 1 {
		t.Fatalf("bilder %d", poll.Bilder)
	}
	if countBilder(t, svc) != 1 {
		t.Fatalf("stored images %d", countBilder(t, svc))
	}
}

// BELEGAPP_MAIL_MAX_MESSAGES_PER_POLL bounds one poll; the rest of the backlog
// waits for the next one. Without a limit of its own the intake takes the
// documented default.
func TestIngestMailboxHonoursThePollLimit(t *testing.T) {
	ctx := context.Background()
	messages := []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 81, 0, 0)),
		mailWith(2, "kasse@example.de", jpegAttachment(t, 82, 0, 0)),
		mailWith(3, "kasse@example.de", jpegAttachment(t, 83, 0, 0)),
	}

	t.Run("operator limit", func(t *testing.T) {
		box := &mailinbox.Fake{Messages: messages}
		svc := mailFixture(t, box, "kasse@example.de")
		svc.MailMaxMessages = 2

		poll, err := svc.IngestMailbox(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if poll.Geprueft != 2 || poll.Bilder != 2 {
			t.Fatalf("poll %+v", poll)
		}
		rest, err := svc.IngestMailbox(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if rest.Geprueft != 1 || rest.Bilder != 1 {
			t.Fatalf("second poll %+v", rest)
		}
		if countBilder(t, svc) != 3 {
			t.Fatalf("stored images %d", countBilder(t, svc))
		}
	})

	t.Run("default", func(t *testing.T) {
		box := &mailinbox.Fake{Messages: messages}
		svc := mailFixture(t, box, "kasse@example.de")
		poll, err := svc.IngestMailbox(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if poll.Geprueft != 3 {
			t.Fatalf("poll %+v", poll)
		}
	})
}

// The disposition: flag the message, and move it only when a done folder is set.
func TestIngestMailboxDispositionSeenAndMove(t *testing.T) {
	ctx := context.Background()

	t.Run("no done folder only flags", func(t *testing.T) {
		box := &mailinbox.Fake{Messages: []mailinbox.Message{
			mailWith(7, "kasse@example.de", jpegAttachment(t, 11, 11, 11)),
		}}
		svc := mailFixture(t, box, "kasse@example.de")
		if _, err := svc.IngestMailbox(ctx); err != nil {
			t.Fatal(err)
		}
		if len(box.Seen) != 1 || box.Seen[0] != 7 {
			t.Fatalf("seen %v", box.Seen)
		}
		if box.MoveCalls != 0 {
			t.Fatalf("moved %d times without a done folder", box.MoveCalls)
		}
	})

	t.Run("done folder moves", func(t *testing.T) {
		box := &mailinbox.Fake{Messages: []mailinbox.Message{
			mailWith(8, "kasse@example.de", jpegAttachment(t, 12, 12, 12)),
		}}
		svc := mailFixture(t, box, "kasse@example.de")
		svc.MailDoneFolder = "Belege/erledigt"
		if _, err := svc.IngestMailbox(ctx); err != nil {
			t.Fatal(err)
		}
		if box.MovedTo != "Belege/erledigt" || !box.MovedUID(8) {
			t.Fatalf("moved to %q uid8=%v", box.MovedTo, box.MovedUID(8))
		}
	})
}

// A move that fails is a filing problem, not a lost receipt: the images are
// stored and the messages are flagged, so the poll does not run again on them.
func TestIngestMailboxMoveFailureIsNotFatal(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{
		Messages: []mailinbox.Message{mailWith(9, "kasse@example.de", jpegAttachment(t, 13, 13, 13))},
		MoveErr:  &mailinbox.Error{Kind: mailinbox.KindPermanent, Text: "Ordner fehlt"},
	}
	svc := mailFixture(t, box, "kasse@example.de")
	svc.MailDoneFolder = "gibt-es-nicht"

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatalf("a failed move must not fail the job: %v", err)
	}
	if poll.Bilder != 1 || len(box.Seen) != 1 {
		t.Fatalf("poll %+v seen %v", poll, box.Seen)
	}
}

// The idempotency contract: the second poll finds nothing, because the first
// one marked the message. This is what keeps a retry or a restart from
// producing a second copy of every receipt.
func TestIngestMailboxIsIdempotentAcrossPolls(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 21, 21, 21)),
		mailWith(2, "kasse@example.de", jpegAttachment(t, 22, 22, 22)),
	}}
	svc := mailFixture(t, box, "kasse@example.de")

	first, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.Bilder != 2 || countBilder(t, svc) != 2 {
		t.Fatalf("first poll %+v stored %d", first, countBilder(t, svc))
	}

	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Geprueft != 0 || second.Bilder != 0 {
		t.Fatalf("second poll %+v", second)
	}
	if countBilder(t, svc) != 2 {
		t.Fatalf("second poll duplicated an image: %d", countBilder(t, svc))
	}
}

// One unusable attachment must not cost the poll the rest of the mail. A
// corrupt file is refused by SaveBild on every read of those bytes, so the
// message is consumed anyway — retrying it would only add a second copy of the
// picture next to it — and the messages behind it are still read.
func TestIngestMailboxBadAttachmentDoesNotStallOrDuplicate(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 10, 20, 30)),
		// The second message carries a picture and a file that is not one.
		mailWith(2, "kasse@example.de", jpegAttachment(t, 40, 50, 60),
			mailinbox.Attachment{Filename: "kaputt.jpg", MimeType: "image/jpeg", Data: []byte("kein bild")}),
		mailWith(3, "kasse@example.de", jpegAttachment(t, 70, 80, 90)),
	}}
	svc := mailFixture(t, box, "kasse@example.de")

	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatalf("one bad attachment must not fail the poll: %v", err)
	}
	if poll.Geprueft != 3 || poll.Bilder != 3 || poll.Uebersprungen != 1 {
		t.Fatalf("poll %+v", poll)
	}
	if countBilder(t, svc) != 3 {
		t.Fatalf("stored images %d, want one per message", countBilder(t, svc))
	}
	list, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Gesamt != 3 || len(list.Bilder) != 3 {
		t.Fatalf("offene bilder %+v", list)
	}
	if len(box.Seen) != 3 {
		t.Fatalf("seen %v, want all three consumed", box.Seen)
	}

	// The retry a restart or a failed job would make finds nothing and adds
	// no second copy of anything.
	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Geprueft != 0 || second.Bilder != 0 {
		t.Fatalf("second poll %+v", second)
	}
	if countBilder(t, svc) != 3 {
		t.Fatalf("the second poll duplicated an image: %d", countBilder(t, svc))
	}
}

// The other half. A storage failure is not a property of the attachment, so it
// ends the poll — but the message before it is already on disk, so it has to be
// flagged before the error leaves. Otherwise the job retry reads it again and
// every receipt in the batch is ingested twice.
func TestIngestMailboxStorageFailureDoesNotDuplicateEarlierImages(t *testing.T) {
	ctx := context.Background()
	// One image is two puts — the normalised JPEG and its thumbnail — so the
	// second message is the one that finds the store down.
	store := newFailingStore(t, 2)
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 15, 25, 35)),
		mailWith(2, "kasse@example.de", jpegAttachment(t, 45, 55, 65)),
		mailWith(3, "kasse@example.de", jpegAttachment(t, 75, 85, 95)),
	}}
	svc := mailFixture(t, box, "kasse@example.de")
	svc.Store = store

	poll, err := svc.IngestMailbox(ctx)
	if err == nil {
		t.Fatal("expected the storage failure to surface")
	}
	if poll.Bilder != 1 {
		t.Fatalf("poll %+v", poll)
	}
	if len(box.Seen) != 1 || box.Seen[0] != 1 {
		t.Fatalf("seen %v, want only the message that made it to disk", box.Seen)
	}

	// The storage backend recovers and the job retries. The first message must
	// not come back.
	store.recover()
	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Geprueft != 2 || second.Bilder != 2 {
		t.Fatalf("second poll %+v", second)
	}
	if countBilder(t, svc) != 3 {
		t.Fatalf("stored images %d, want one per message and no second copy", countBilder(t, svc))
	}
	list, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Gesamt != 3 || len(list.Bilder) != 3 {
		t.Fatalf("offene bilder %+v", list)
	}

	// A third poll proves the flag of the first message is still standing: the
	// intake remembers every message it consumed, not only the last batch it
	// happened to mark.
	third, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if third.Geprueft != 0 || third.Bilder != 0 {
		t.Fatalf("third poll %+v; message 1 was consumed in the first one", third)
	}
	if countBilder(t, svc) != 3 {
		t.Fatalf("stored images %d, want no second copy", countBilder(t, svc))
	}
}

// The half of the contract the single-attachment messages above cannot reach: a
// message whose *second* attachment hits the store. The first attachment is
// already committed when that happens, and a message that is not flagged is
// read again from the top on the next poll — which would store that first
// image a second time on every interval. So the interrupted message is consumed
// here, and the two attachments the poll could not take are counted as skipped
// rather than dropped without a trace.
func TestIngestMailboxPartiallyStoredMessageIsNotReadAgain(t *testing.T) {
	ctx := context.Background()
	// One image is two puts, so a budget of four stores the whole first message
	// and the first attachment of the second, and fails on its second.
	store := newFailingStore(t, 4)
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 15, 25, 35)),
		mailWith(2, "kasse@example.de",
			jpegAttachment(t, 45, 55, 65),
			jpegAttachment(t, 46, 56, 66),
			jpegAttachment(t, 47, 57, 67),
		),
		mailWith(3, "kasse@example.de", jpegAttachment(t, 75, 85, 95)),
	}}
	svc := mailFixture(t, box, "kasse@example.de")
	svc.Store = store

	poll, err := svc.IngestMailbox(ctx)
	if err == nil {
		t.Fatal("expected the storage failure to surface")
	}
	// Message 1 in full and the first attachment of message 2.
	if poll.Bilder != 2 {
		t.Fatalf("poll %+v, want the two images that reached the store", poll)
	}
	// The attachment that failed and the one behind it: not stored, but
	// accounted for, so the poll does not claim a receipt it only read in part.
	if poll.Uebersprungen != 2 {
		t.Fatalf("poll %+v, want the two unstored attachments counted as skipped", poll)
	}
	// The regression itself: message 2 has an image on disk, so it counts as
	// consumed. Without this it would be handed back by the next poll.
	if len(box.Seen) != 2 || box.Seen[0] != 1 || box.Seen[1] != 2 {
		t.Fatalf("seen %v, want the partially stored message flagged as well", box.Seen)
	}
	if countBilder(t, svc) != 2 {
		t.Fatalf("stored images %d", countBilder(t, svc))
	}

	// The store recovers and the job retries. The two images already on disk
	// must not appear a second time, and the message behind the failure still
	// has to be read — the poll stops at the error, not at that message.
	store.recover()
	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Geprueft != 1 || second.Bilder != 1 {
		t.Fatalf("second poll %+v, want only the message after the failure", second)
	}
	// Three images in total: one per message, plus nothing for the two
	// attachments of message 2 that were counted as skipped above.
	if countBilder(t, svc) != 3 {
		t.Fatalf("stored images %d, want 3 — a fourth would be a duplicated image", countBilder(t, svc))
	}
	list, err := svc.ListOffeneBilder(ctx, OffeneBilderParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Gesamt != 3 || len(list.Bilder) != 3 {
		t.Fatalf("offene bilder %+v", list)
	}

	// And nothing moves again once every message is consumed.
	third, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if third.Geprueft != 0 || third.Bilder != 0 {
		t.Fatalf("third poll %+v", third)
	}
	if countBilder(t, svc) != 3 {
		t.Fatalf("stored images %d, want no second copy", countBilder(t, svc))
	}
}

// The other end of the same rule. A store that is down before the first
// attachment has committed nothing, so there is nothing a later poll could
// duplicate — flagging the message would only throw the whole receipt away for
// the length of an outage. It stays in the queue and the next poll reads it
// from the top.
func TestIngestMailboxStorageFailureBeforeAnyImageKeepsTheMessage(t *testing.T) {
	ctx := context.Background()
	store := newFailingStore(t, 0)
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de",
			jpegAttachment(t, 15, 25, 35),
			jpegAttachment(t, 45, 55, 65),
		),
	}}
	svc := mailFixture(t, box, "kasse@example.de")
	svc.Store = store

	poll, err := svc.IngestMailbox(ctx)
	if err == nil {
		t.Fatal("expected the storage failure to surface")
	}
	if poll.Bilder != 0 || poll.Uebersprungen != 2 {
		t.Fatalf("poll %+v", poll)
	}
	if len(box.Seen) != 0 {
		t.Fatalf("seen %v, want nothing flagged while no image is stored", box.Seen)
	}

	store.recover()
	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Bilder != 2 {
		t.Fatalf("second poll %+v, want the message read from its first attachment", second)
	}
	if countBilder(t, svc) != 2 {
		t.Fatalf("stored images %d, want one per attachment", countBilder(t, svc))
	}
}

// errFlagFailed is what a store the server refuses looks like from here. One
// dropped packet on the IMAP STORE command is enough to produce it, and nothing
// about the message on the wire changes.
var errFlagFailed = errors.New("the marker store did not survive the connection")

// The flag is a network call like any other and fails on its own account. The
// keyword it writes is the only note of a consumed message that outlives the
// poll, so a flag that never lands hands the same messages to the next poll —
// and SaveBild does not dedupe, so every picture behind it would reach the blob
// store a second time. This process therefore remembers the messages whose
// images it stored until the server takes the flag, and only offers them to the
// marker until then.
func TestIngestMailboxMarkFailureDoesNotDuplicateStoredImages(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{
		Messages: []mailinbox.Message{
			mailWith(1, "kasse@example.de",
				jpegAttachment(t, 15, 25, 35),
				jpegAttachment(t, 45, 55, 65),
			),
		},
		MarkErr: errFlagFailed,
	}
	svc := mailFixture(t, box, "kasse@example.de")

	poll, err := svc.IngestMailbox(ctx)
	if err == nil {
		t.Fatal("expected the failed flag to surface")
	}
	if poll.Bilder != 2 {
		t.Fatalf("poll %+v", poll)
	}
	if len(box.Seen) != 0 {
		t.Fatalf("seen %v, the flag did not stick", box.Seen)
	}
	if countBilder(t, svc) != 2 {
		t.Fatalf("stored images %d", countBilder(t, svc))
	}

	// The server takes the flag again. The message is still unmarked, so the
	// next poll is handed it once more — but its pictures are on disk already,
	// and the second copy is exactly what this guards against.
	box.MarkErr = nil
	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Geprueft != 1 || second.Bilder != 0 || second.Uebersprungen != 0 {
		t.Fatalf("second poll %+v, want the message read and nothing stored", second)
	}
	if got := countBilder(t, svc); got != 2 {
		t.Fatalf("stored images %d, want the two of the first poll and no second copy", got)
	}
	// The note is not a second disposition: the message is still offered to the
	// marker, so it leaves the queue for good the moment the server accepts one.
	if len(box.Seen) != 1 || box.Seen[0] != 1 {
		t.Fatalf("seen %v, want the stored message flagged now that the server takes the marker", box.Seen)
	}

	third, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if third.Geprueft != 0 || third.Bilder != 0 {
		t.Fatalf("third poll %+v", third)
	}
	if countBilder(t, svc) != 2 {
		t.Fatalf("stored images %d, want no second copy", countBilder(t, svc))
	}
}

// The other end of the same rule. Nothing reached the disk, so there is nothing
// a retry could duplicate, and the receipt has to outlive an outage rather than
// be marked done against a picture that does not exist.
func TestIngestMailboxMarkFailureWithoutStoredImagesKeepsTheMessage(t *testing.T) {
	ctx := context.Background()
	store := newFailingStore(t, 0)
	box := &mailinbox.Fake{
		Messages: []mailinbox.Message{
			mailWith(1, "kasse@example.de",
				jpegAttachment(t, 15, 25, 35),
				jpegAttachment(t, 45, 55, 65),
			),
		},
		MarkErr: errFlagFailed,
	}
	svc := mailFixture(t, box, "kasse@example.de")
	svc.Store = store

	poll, err := svc.IngestMailbox(ctx)
	if err == nil {
		t.Fatal("expected the store failure to surface")
	}
	if poll.Bilder != 0 {
		t.Fatalf("poll %+v", poll)
	}
	if len(box.Seen) != 0 {
		t.Fatalf("seen %v, want nothing flagged", box.Seen)
	}

	store.recover()
	box.MarkErr = nil
	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Bilder != 2 {
		t.Fatalf("second poll %+v, want the message read from its first attachment", second)
	}
	if countBilder(t, svc) != 2 {
		t.Fatalf("stored images %d, want one per attachment", countBilder(t, svc))
	}
	if len(box.Seen) != 1 || box.Seen[0] != 1 {
		t.Fatalf("seen %v, want the message flagged on the retry", box.Seen)
	}
}

// A message the service read and kept no picture from is consumed like any
// other — the same bytes refuse the same way on every read. It still may not go
// into the note, because there is nothing behind it to duplicate and the marker
// is the only thing that ends the queue entry: remembering it would drop the
// message for good after a flag failure that had nothing to do with it.
func TestIngestMailboxMarkFailureWithoutImagesIsNotRemembered(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{
		Messages: []mailinbox.Message{
			mailWith(1, "kasse@example.de",
				mailinbox.Attachment{Filename: "beleg.pdf", MimeType: "application/pdf", Data: []byte("%PDF")}),
		},
		MarkErr: errFlagFailed,
	}
	svc := mailFixture(t, box, "kasse@example.de")

	if _, err := svc.IngestMailbox(ctx); err == nil {
		t.Fatal("expected the failed flag to surface")
	}
	if len(box.Seen) != 0 {
		t.Fatalf("seen %v", box.Seen)
	}

	box.MarkErr = nil
	second, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Geprueft != 1 || second.Uebersprungen != 1 {
		t.Fatalf("second poll %+v, want the message read again", second)
	}
	if len(box.Seen) != 1 || box.Seen[0] != 1 {
		t.Fatalf("seen %v, want the message flagged on the retry", box.Seen)
	}
}

// The note itself. It is what stands in for a marker the server would not take,
// so it has to answer the one question the poll asks it — is this message
// already on disk here? — and to shrink again as soon as the server has the
// flag, or it would grow with the mailbox instead of with the outage.
func TestMailConsumed(t *testing.T) {
	var note mailConsumed
	if note.has(1) {
		t.Fatal("an empty note holds nothing")
	}
	note.add([]imap.UID{1, 2, 1})
	if !note.has(1) || !note.has(2) || note.has(3) {
		t.Fatalf("note %v, want 1 and 2", note.uids)
	}
	if len(note.uids) != 2 {
		t.Fatalf("note %v, want one entry per UID", note.uids)
	}
	// The poll offers the remembered messages to the marker again, so the note
	// has to keep them while the server refuses and keep the order it stored
	// them in.
	note.add([]imap.UID{2})
	if len(note.uids) != 2 {
		t.Fatalf("note %v, want no second entry for a UID already in it", note.uids)
	}
	note.forget([]imap.UID{1, 9})
	if note.has(1) {
		t.Fatal("a flagged message has to leave the note")
	}
	if !note.has(2) {
		t.Fatal("a message the server still has not flagged has to stay")
	}
	note.forget([]imap.UID{2})
	if note.has(2) || len(note.uids) != 0 {
		t.Fatalf("note %v, want empty once the server has every flag", note.uids)
	}
	// The empty operations of an untouched note must not allocate anything.
	note.add(nil)
	note.forget(nil)
	if note.has(1) || len(note.uids) != 0 {
		t.Fatalf("note %v", note.uids)
	}
}

// failingStore lets puts through until its budget runs out and fails every
// later one; recover gives the budget back. That is how a test breaks the blob
// store in the middle of a poll and heals it again.
type failingStore struct {
	storage.BlobStore
	mu     sync.Mutex
	spent  int
	budget int
}

func newFailingStore(t *testing.T, budget int) *failingStore {
	t.Helper()
	return &failingStore{BlobStore: storage.NewFS(t.TempDir()), budget: budget}
}

func (s *failingStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	s.mu.Lock()
	s.spent++
	ok := s.spent <= s.budget
	s.mu.Unlock()
	if !ok {
		return errors.New("blob store is not reachable")
	}
	return s.BlobStore.Put(ctx, key, r, size, contentType)
}

// recover tops the budget up by far more than the rest of the test needs.
func (s *failingStore) recover() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.budget = s.spent + 1<<20
}

// The retry classification the job queue reads is the caller's contract:
// auth failures stop, transport failures repeat.
func TestIngestMailboxErrorClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"auth", &mailinbox.Error{Kind: mailinbox.KindAuth, Text: "Zugang abgelehnt"}, mailinbox.KindAuth},
		{"transient", &mailinbox.Error{Kind: mailinbox.KindTransient, Text: "nicht erreichbar"}, mailinbox.KindTransient},
		{"permanent", &mailinbox.Error{Kind: mailinbox.KindPermanent, Text: "Ordner fehlt"}, mailinbox.KindPermanent},
		{"unset kind defaults to transient", &mailinbox.Error{Text: "ohne Angabe"}, mailinbox.KindTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			box := &mailinbox.Fake{FetchErr: tt.err}
			svc := mailFixture(t, box, "kasse@example.de")

			_, err := svc.IngestMailbox(ctx)
			if !errors.Is(err, tt.err) {
				t.Fatalf("got %v, want the source error back", err)
			}
			var kind interface{ RetryKind() string }
			if !errors.As(err, &kind) {
				t.Fatal("the job queue could not classify the error")
			}
			if got := kind.RetryKind(); got != tt.want {
				t.Fatalf("retry kind %q, want %q", got, tt.want)
			}
			if !shouldRetryMail(kind.RetryKind(), 1) == (tt.want == mailinbox.KindTransient) {
				t.Fatalf("retry decision disagrees with kind %q", tt.want)
			}
		})
	}
}

// shouldRetryMail mirrors jobs.shouldRetry so the classification is asserted
// against the behaviour the queue really has, not a copy of it.
func shouldRetryMail(kind string, attempt int) bool {
	switch kind {
	case mailinbox.KindAuth, mailinbox.KindPermanent:
		return false
	default:
		return attempt < 3
	}
}

func TestIngestMailboxWithoutSourceIsNoop(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, recognition.Fake{})
	poll, err := svc.IngestMailbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poll != (MailPoll{}) {
		t.Fatalf("poll %+v", poll)
	}
}

// The mail job goes through HandleJob like every other type, and its result is
// the poll summary the job endpoint serves.
func TestHandleJobMail(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 41, 41, 41)),
		mailWith(2, "fremd@example.de", jpegAttachment(t, 42, 42, 42)),
	}}
	svc := mailFixture(t, box, "kasse@example.de")

	_, result, err := svc.HandleJob(ctx, jobs.Job{ID: "job-1", Typ: "mail", Payload: mailJobPayload})
	if err != nil {
		t.Fatal(err)
	}
	var poll MailPoll
	if err := json.Unmarshal([]byte(result), &poll); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}
	if poll.Bilder != 1 || poll.Abgelehnt != 1 || poll.Geprueft != 2 {
		t.Fatalf("poll %+v", poll)
	}
}

// Without a mailbox no ticker runs and no job is enqueued: an instance that
// does not use the feature never grows a queue row.
func TestPollMailboxNoSourceEnqueuesNothing(t *testing.T) {
	svc := openService(t, recognition.Fake{})
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: testPoll}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.PollMailbox(ctx)

	var n int
	if err := svc.DB.Read.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("jobs %d without a mailbox", n)
	}
}

func TestPollMailboxEnqueuesOnEveryTick(t *testing.T) {
	box := &mailinbox.Fake{}
	svc := mailFixture(t, box)
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: testPoll, Handle: svc.HandleJob}
	svc.MailInterval = testIntakeInterval

	ctx, cancel := context.WithCancel(context.Background())
	queueDone := make(chan struct{})
	go func() {
		svc.RunJobs(ctx)
		close(queueDone)
	}()
	// The queue drains each poll, so the next tick is free to queue the next.
	done := make(chan struct{})
	go func() {
		svc.PollMailbox(ctx)
		close(done)
	}()
	// Waiting for the second row ends the test when the ticker has done what it
	// is about; a fixed sleep only hoped it had, and fails on a loaded CI box.
	dbtest.WaitFor(t, 10*time.Second, "a second mail job", func() bool {
		return countJobs(t, svc, "mail") >= 2
	})
	cancel()
	<-done
	<-queueDone
}

// An open mail job makes the next tick a no-op. Without that a server outage
// would add one job per interval on top of the retries already in the queue.
func TestPollMailboxSkipsTickWhileAJobIsOpen(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{}
	svc := mailFixture(t, box)
	// The queue never runs, so the first job stays open for the whole window.
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: testPoll}
	svc.MailInterval = testIntakeInterval

	poller, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		svc.PollMailbox(poller)
		close(done)
	}()
	// Wait for the first row, then watch five whole intervals for a second one.
	jobsStayAt(t, svc, "mail", 1, svc.MailInterval)
	cancel()
	<-done
}

// SaveBild enqueues the recognition job itself, so the intake must not do it a
// second time. Recognition is off in this fixture, so the count is checked
// through the queue directly.
func TestIngestMailboxDoesNotEnqueueRecognitionTwice(t *testing.T) {
	ctx := context.Background()
	box := &mailinbox.Fake{Messages: []mailinbox.Message{
		mailWith(1, "kasse@example.de", jpegAttachment(t, 51, 51, 51)),
	}}
	svc := mailFixture(t, box, "kasse@example.de")
	// The setting row says recognition is on, so SaveBild queues the job.
	if _, err := svc.DB.Write.ExecContext(ctx,
		`UPDATE einstellungen SET erkennung_aktiv = 1`); err != nil {
		t.Fatal(err)
	}
	svc.Jobs = &jobs.Queue{DB: svc.DB, Workers: 1, Poll: testPoll}

	if _, err := svc.IngestMailbox(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := svc.DB.Read.QueryRow(`SELECT COUNT(*) FROM jobs WHERE typ = 'erkennung'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recognition jobs %d, want exactly one", n)
	}
}

// What the intake is allowed to put into the log. The Service never holds the
// IMAP password — it is not a field and not reachable from a Message — so the
// secret this guards is the mail itself: no log line may carry a body, an
// attachment, or the decoded message as a whole, only the fields listed below.
// (The password is asserted where it actually lives, in the IMAP source, by
// TestIMAPWrongPasswordIsAuth.)
// The job row outlives the process and is the one place a mailbox failure is
// stored as text. The source classifies before it returns, so what the queue
// writes is the fixed German sentence — the server's own words, the host, the
// port and the password stay behind Error.Unwrap where nothing prints them.
// This asserts the end of that chain on the row itself, not on the error value.
func TestMailJobRowCarriesNoServerText(t *testing.T) {
	// The shape a real poll produces: a classified *Error around the raw
	// *imap.Error, whose Text is the server response verbatim.
	box := &mailinbox.Fake{FetchErr: &mailinbox.Error{
		Kind: mailinbox.KindTransient,
		Text: "Der Mailserver antwortet mit einem Fehler",
		Err: &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "mailbox is locked [PRIVACYREQUIRED] at imap.example.de:993 " +
				"from 203.0.113.9 (app-password, tk_geheim)",
		},
	}}
	svc := openService(t, recognition.Fake{})
	svc.Mail = box
	var buf syncBuffer
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	startJobs(t, svc)

	id, err := svc.Jobs.Enqueue(context.Background(), "mail", mailJobPayload)
	if err != nil {
		t.Fatal(err)
	}
	row := waitJobStatus(t, svc, id, "fehler")
	reason, _ := row.Fehler.(string)
	needles := []string{
		"imap.example.de", "203.0.113.9", ":993", "app-password",
		"tk_geheim", "mailbox is locked", "PRIVACYREQUIRED",
	}
	for _, needle := range needles {
		if strings.Contains(reason, needle) {
			t.Errorf("jobs.fehler carries %q: %s", needle, reason)
		}
		if strings.Contains(buf.String(), needle) {
			t.Errorf("the log carries %q: %s", needle, buf.String())
		}
	}
	// The operator still gets something to read: a row with an empty reason
	// would say nothing at all about why the poll failed.
	if strings.TrimSpace(reason) == "" {
		t.Errorf("jobs.fehler is empty; the operator learns nothing")
	}
}

// waitJobStatus blocks until the job row reaches status and returns it.
func waitJobStatus(t *testing.T, svc *Service, id, status string) db.Job {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	var last db.Job
	for time.Now().Before(deadline) {
		row, err := db.New(svc.DB.Read).GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		last = row
		if row.Status == status {
			return row
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("job status %q, want %q (%v)", last.Status, status, last.Fehler)
	return last
}

func TestIngestMailboxLogCarriesSenderAndUIDOnly(t *testing.T) {
	ctx := context.Background()
	var buf syncBuffer
	attachment := jpegAttachment(t, 61, 61, 61)
	box := &mailinbox.Fake{
		Messages: []mailinbox.Message{
			// Rejected sender.
			mailWith(1, "fremd@example.de", attachment),
			// Unsupported type, broken transfer, and one stored image.
			mailWith(2, "kasse@example.de",
				mailinbox.Attachment{Filename: "bon.pdf", MimeType: "application/pdf", Data: []byte("%PDF-1.7")},
				mailinbox.Attachment{Filename: "kaputt.jpg", MimeType: "image/jpeg", Err: errors.New("connection reset")},
				attachment,
			),
			// Nothing this application stores.
			mailWith(3, "kasse@example.de",
				mailinbox.Attachment{Filename: "nur-text.txt", MimeType: "text/plain", Data: []byte("kein Beleg")}),
		},
		MoveErr: &mailinbox.Error{Kind: mailinbox.KindPermanent, Text: "Ordner fehlt"},
	}
	svc := mailFixture(t, box, "kasse@example.de")
	svc.MailDoneFolder = "gibt-es-nicht"
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))

	if _, err := svc.IngestMailbox(ctx); err != nil {
		t.Fatal(err)
	}

	records := logRecords(t, buf.String())
	// Rejected sender, unsupported type, broken transfer, a message without a
	// picture, and the failed filing.
	if len(records) < 5 {
		t.Fatalf("%d log lines, want one per finding: %s", len(records), buf.String())
	}
	rejected := 0
	for _, record := range records {
		keys := attributeKeys(record)
		if len(keys) == 0 {
			t.Fatalf("log line without attributes: %v", record)
		}
		for _, key := range keys {
			if !mailLogKeys[key] {
				t.Errorf("log line carries the attribute %q, which is not on the list: %v", key, record)
			}
		}
		for key, value := range record {
			if len(value) > maxLogValue {
				t.Errorf("the attribute %q carries %d bytes; a body or a picture must never be logged", key, len(value))
			}
		}
		if sender, ok := record["absender"]; ok {
			rejected++
			if len(keys) != 2 {
				t.Errorf("the rejected-sender line carries %v; it may name the sender and the UID and nothing else", keys)
			}
			if sender != "fremd@example.de" {
				t.Errorf("absender %q", sender)
			}
		}
	}
	if rejected != 1 {
		t.Errorf("%d lines name the rejected sender, want exactly 1", rejected)
	}
}

// mailLogKeys are the only attributes the mail intake may log. Anything else —
// a decoded message, a body, a source configuration — would be new information
// in a line that ends up in the operator's log.
var mailLogKeys = map[string]bool{
	"absender":    true,
	"uid":         true,
	"betreff":     true,
	"datei":       true,
	"typ":         true,
	"grenze":      true,
	"err":         true,
	"ordner":      true,
	"bilder":      true,
	"nachrichten": true,
}

// maxLogValue is the longest attribute value an intake log line may carry. A
// sender address, a file name, a subject line and an error message all fit; a
// mail body or a picture does not.
const maxLogValue = 256

// logRecords parses the JSON handler output into one map per line. The
// handler's own fields are kept so the key list stays a strict allowlist.
func logRecords(t *testing.T, text string) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		record := make(map[string]string, len(fields))
		for key, value := range fields {
			printed, ok := value.(string)
			if !ok {
				printed = fmt.Sprint(value)
			}
			record[key] = printed
		}
		out = append(out, record)
	}
	return out
}

// attributeKeys are the keys of a log record without the handler's own fields.
func attributeKeys(record map[string]string) []string {
	var out []string
	for key := range record {
		switch key {
		case "time", "level", "msg":
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// syncBuffer is a bytes.Buffer that survives the service's goroutines under
// -race. The repository has no sync buffer for this, and adding one would be
// out of scope for this unit.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

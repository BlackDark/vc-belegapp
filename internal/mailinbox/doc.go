// Package mailinbox reads new mail from an IMAP mailbox and hands the service
// layer plain attachments.
//
// The service never sees an IMAP type. It talks to the Source interface, which
// an in-memory fake implements for the tests and the go-imap client implements
// for real servers. The one rule that shapes the whole package: this code never
// deletes mail. It sets \Seen and a keyword of its own, and it may MOVE a
// message into the folder the operator named. Nothing sets \Deleted and nothing
// calls EXPUNGE.
package mailinbox

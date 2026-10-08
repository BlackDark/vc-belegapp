package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
)

const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Entry is one append-only change, without the hash (computed on insert).
type Entry struct {
	ID         int64
	Zeitpunkt  string
	Akteur     string
	Aktion     string
	Entitaet   string
	EntitaetID string
	Monat      string
	Vorher     json.RawMessage
	Nachher    json.RawMessage
	Diff       json.RawMessage
	Grund      string
	RequestID  string
	PrevHash   string
	Hash       string
}

// Report is the result of a chain check.
type Report struct {
	OK             bool
	Anzahl         int
	ErsterFehlerID int64
}

// Append writes one entry in the caller's transaction and sets ID, PrevHash and Hash.
func Append(ctx context.Context, tx *sql.Tx, e *Entry) error {
	if e == nil {
		return errors.New("audit: nil entry")
	}
	var prev string
	var lastID int64
	err := tx.QueryRowContext(ctx, `SELECT id, hash FROM aenderungsprotokoll ORDER BY id DESC LIMIT 1`).Scan(&lastID, &prev)
	if errors.Is(err, sql.ErrNoRows) {
		prev = zeroHash
		lastID = 0
	} else if err != nil {
		return err
	}
	e.ID = lastID + 1
	e.PrevHash = prev
	sum, err := Hash(e)
	if err != nil {
		return err
	}
	e.Hash = sum
	_, err = tx.ExecContext(ctx, `
		INSERT INTO aenderungsprotokoll (
			id, zeitpunkt, akteur, aktion, entitaet, entitaet_id, monat,
			vorher, nachher, diff, grund, request_id, prev_hash, hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Zeitpunkt, e.Akteur, e.Aktion, e.Entitaet, e.EntitaetID, nullString(e.Monat),
		nullRaw(e.Vorher), nullRaw(e.Nachher), nullRaw(e.Diff), nullString(e.Grund), e.RequestID, e.PrevHash, e.Hash,
	)
	return err
}

// List returns entries in descending id order.
func List(ctx context.Context, db queryer, monat, entitaetID string, vorID int64, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, zeitpunkt, akteur, aktion, entitaet, entitaet_id, monat,
		       vorher, nachher, diff, grund, request_id, prev_hash, hash
		FROM aenderungsprotokoll
		WHERE (? = '' OR monat = ?)
		  AND (? = '' OR entitaet_id = ?)
		  AND (? = 0 OR id < ?)
		ORDER BY id DESC
		LIMIT ?`, monat, monat, entitaetID, entitaetID, vorID, vorID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanAll(rows)
}

// Verify checks the hash chain from the first entry.
func Verify(ctx context.Context, db queryer) (Report, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, zeitpunkt, akteur, aktion, entitaet, entitaet_id, monat,
		       vorher, nachher, diff, grund, request_id, prev_hash, hash
		FROM aenderungsprotokoll
		ORDER BY id ASC`)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = rows.Close() }()
	entries, err := scanAll(rows)
	if err != nil {
		return Report{}, err
	}
	prev := zeroHash
	for _, entry := range entries {
		if entry.PrevHash != prev {
			return Report{Anzahl: len(entries), ErsterFehlerID: entry.ID}, nil
		}
		sum, err := Hash(&entry)
		if err != nil {
			return Report{}, err
		}
		if sum != entry.Hash {
			return Report{Anzahl: len(entries), ErsterFehlerID: entry.ID}, nil
		}
		prev = entry.Hash
	}
	return Report{OK: true, Anzahl: len(entries)}, nil
}

// Hash is hex(sha256(prev_hash + "\n" + canonical JSON without the hash field)).
func Hash(e *Entry) (string, error) {
	body, err := canonical(e.document())
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(e.PrevHash + "\n" + string(body)))
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON returns the specification's canonical encoding.
func CanonicalJSON(v any) ([]byte, error) {
	return canonical(v)
}

// DiffJSON returns a canonical JSON array of {feld, alt, neu} for keys that differ.
func DiffJSON(before, after json.RawMessage) (json.RawMessage, error) {
	left, err := decodeObject(before)
	if err != nil {
		return nil, err
	}
	right, err := decodeObject(after)
	if err != nil {
		return nil, err
	}
	keys := map[string]struct{}{}
	for key := range left {
		keys[key] = struct{}{}
	}
	for key := range right {
		keys[key] = struct{}{}
	}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	changes := make([]any, 0)
	for _, key := range names {
		a, aOK := left[key]
		b, bOK := right[key]
		ab, _ := canonical(nilIfAbsent(a, aOK))
		bb, _ := canonical(nilIfAbsent(b, bOK))
		if string(ab) == string(bb) {
			continue
		}
		changes = append(changes, map[string]any{
			"feld": key,
			"alt":  nilIfAbsent(a, aOK),
			"neu":  nilIfAbsent(b, bOK),
		})
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return canonical(changes)
}

// Snapshot encodes a value as canonical JSON for vorher/nachher.
func Snapshot(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return nil, err
	}
	return canonical(decoded)
}

func (e *Entry) document() map[string]any {
	return map[string]any{
		"aktion":      e.Aktion,
		"akteur":      e.Akteur,
		"diff":        rawValue(e.Diff),
		"entitaet":    e.Entitaet,
		"entitaet_id": e.EntitaetID,
		"grund":       nilIfEmpty(e.Grund),
		"id":          e.ID,
		"monat":       nilIfEmpty(e.Monat),
		"nachher":     rawValue(e.Nachher),
		"prev_hash":   e.PrevHash,
		"request_id":  e.RequestID,
		"vorher":      rawValue(e.Vorher),
		"zeitpunkt":   e.Zeitpunkt,
	}
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func scanAll(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		var e Entry
		var monat, vorher, nachher, diff, grund sql.NullString
		if err := rows.Scan(
			&e.ID, &e.Zeitpunkt, &e.Akteur, &e.Aktion, &e.Entitaet, &e.EntitaetID, &monat,
			&vorher, &nachher, &diff, &grund, &e.RequestID, &e.PrevHash, &e.Hash,
		); err != nil {
			return nil, err
		}
		e.Monat = monat.String
		e.Grund = grund.String
		e.Vorher = rawFromNull(vorher)
		e.Nachher = rawFromNull(nachher)
		e.Diff = rawFromNull(diff)
		out = append(out, e)
	}
	if out == nil {
		out = []Entry{}
	}
	return out, rows.Err()
}

func canonical(v any) ([]byte, error) {
	var buf []byte
	err := writeCanonical(&buf, v)
	return buf, err
}

func writeCanonical(buf *[]byte, v any) error {
	switch t := v.(type) {
	case nil:
		*buf = append(*buf, "null"...)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for key := range t {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		*buf = append(*buf, '{')
		for i, key := range keys {
			if i > 0 {
				*buf = append(*buf, ',')
			}
			writeString(buf, key)
			*buf = append(*buf, ':')
			if err := writeCanonical(buf, t[key]); err != nil {
				return err
			}
		}
		*buf = append(*buf, '}')
	case []any:
		*buf = append(*buf, '[')
		for i, item := range t {
			if i > 0 {
				*buf = append(*buf, ',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		*buf = append(*buf, ']')
	case json.Number:
		*buf = append(*buf, t.String()...)
	case string:
		writeString(buf, t)
	case bool:
		*buf = append(*buf, strconv.FormatBool(t)...)
	case float64:
		*buf = append(*buf, strconv.FormatFloat(t, 'f', -1, 64)...)
	case int:
		*buf = append(*buf, strconv.Itoa(t)...)
	case int64:
		*buf = append(*buf, strconv.FormatInt(t, 10)...)
	case json.RawMessage:
		return writeCanonical(buf, rawValue(t))
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			return err
		}
		return writeCanonical(buf, decoded)
	}
	return nil
}

func writeString(buf *[]byte, s string) {
	enc, _ := json.Marshal(s)
	// encoding/json escapes &, <, >. Canonical form keeps them as UTF-8.
	out := make([]byte, 0, len(enc))
	for i := 0; i < len(enc); i++ {
		if enc[i] == '\\' && i+5 < len(enc) && enc[i+1] == 'u' {
			hex := string(enc[i+2 : i+6])
			if hex == "0026" || hex == "003c" || hex == "003e" {
				switch hex {
				case "0026":
					out = append(out, '&')
				case "003c":
					out = append(out, '<')
				default:
					out = append(out, '>')
				}
				i += 5
				continue
			}
		}
		out = append(out, enc[i])
	}
	*buf = append(*buf, out...)
}

func rawValue(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	return v
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if v == nil {
		v = map[string]any{}
	}
	return v, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfAbsent(v any, ok bool) any {
	if !ok {
		return nil
	}
	return v
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullRaw(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func rawFromNull(v sql.NullString) json.RawMessage {
	if !v.Valid || v.String == "" {
		return nil
	}
	return json.RawMessage(v.String)
}

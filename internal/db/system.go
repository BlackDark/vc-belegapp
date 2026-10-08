package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	systemInstanceID = "instanz_id"
	systemSecretKey  = "secret_key"
)

// Runtime holds the instance id and the secret key resolved at startup.
// The secret is the env value when set, otherwise the key stored in system.
type Runtime struct {
	InstanceID string
	SecretKey  []byte
}

// EnsureSystem creates instanz_id and, when envSecret is empty, secret_key.
// An existing secret_key is kept. envSecret is not written to the database.
func EnsureSystem(ctx context.Context, sqlDB *sql.DB, envSecret []byte) (Runtime, error) {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Runtime{}, err
	}
	defer func() { _ = tx.Rollback() }()

	q := New(tx)
	instanceID, err := ensureInstanceID(ctx, q)
	if err != nil {
		return Runtime{}, err
	}
	secret, err := resolveSecret(ctx, q, envSecret)
	if err != nil {
		return Runtime{}, err
	}
	if err := tx.Commit(); err != nil {
		return Runtime{}, err
	}
	return Runtime{InstanceID: instanceID, SecretKey: secret}, nil
}

func ensureInstanceID(ctx context.Context, q *Queries) (string, error) {
	value, err := q.GetSystemValue(ctx, systemInstanceID)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf[:])
	if err := q.UpsertSystemValue(ctx, UpsertSystemValueParams{Key: systemInstanceID, Value: id}); err != nil {
		return "", err
	}
	return id, nil
}

func resolveSecret(ctx context.Context, q *Queries, envSecret []byte) ([]byte, error) {
	if len(envSecret) > 0 {
		if len(envSecret) != 32 {
			return nil, fmt.Errorf("secret key must be 32 bytes")
		}
		return append([]byte(nil), envSecret...), nil
	}
	raw, err := q.GetSystemValue(ctx, systemSecretKey)
	if err == nil {
		decoded, decErr := base64.StdEncoding.DecodeString(raw)
		if decErr != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("stored secret_key is invalid")
		}
		return decoded, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(buf)
	if err := q.UpsertSystemValue(ctx, UpsertSystemValueParams{Key: systemSecretKey, Value: encoded}); err != nil {
		return nil, err
	}
	return buf, nil
}

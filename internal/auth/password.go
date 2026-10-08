package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory  = 64 * 1024
	passwordTime    = 3
	passwordThreads = 2
	passwordKeyLen  = 32
	passwordSaltLen = 16
)

// HashPassword returns an argon2id PHC string (m=65536, t=3, p=2).
func HashPassword(password []byte) (string, error) {
	if len(password) == 0 {
		return "", errors.New("password is empty")
	}
	salt := make([]byte, passwordSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey(password, salt, passwordTime, passwordMemory, passwordThreads, passwordKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		passwordMemory, passwordTime, passwordThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	), nil
}

type phcParams struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	hash    []byte
}

// VerifyPassword compares password with an argon2id PHC string in constant time.
// Parameters are taken from the PHC string.
func VerifyPassword(password, phc string) bool {
	params, err := parsePHC(phc)
	if err != nil {
		dummy := argon2.IDKey([]byte(password), make([]byte, 16), 1, 8*1024, 1, 32)
		return subtle.ConstantTimeCompare(dummy, dummy) == 0
	}
	sum := argon2.IDKey([]byte(password), params.salt, params.time, params.memory, params.threads, uint32(len(params.hash)))
	return subtle.ConstantTimeCompare(sum, params.hash) == 1
}

func parsePHC(phc string) (phcParams, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return phcParams{}, errors.New("not argon2id")
	}
	var memory, timeCost int
	var threads int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return phcParams{}, err
	}
	if memory < 8 || timeCost < 1 || threads < 1 || threads > 255 {
		return phcParams{}, errors.New("invalid argon2 parameters")
	}
	salt, err := decodeB64(parts[4])
	if err != nil {
		return phcParams{}, err
	}
	sum, err := decodeB64(parts[5])
	if err != nil {
		return phcParams{}, err
	}
	if len(salt) == 0 || len(sum) == 0 {
		return phcParams{}, errors.New("empty argon2 material")
	}
	return phcParams{memory: uint32(memory), time: uint32(timeCost), threads: uint8(threads), salt: salt, hash: sum}, nil
}

func decodeB64(s string) ([]byte, error) {
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	out, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	}
	return out, nil
}

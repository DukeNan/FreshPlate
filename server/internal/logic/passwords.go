package logic

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidArgument        = errors.New("INVALID_ARGUMENT")
	ErrInvalidCredentials     = errors.New("INVALID_CREDENTIALS")
	ErrInvalidCurrentPassword = errors.New("INVALID_CURRENT_PASSWORD")
	ErrSamePassword           = errors.New("PASSWORD_UNCHANGED")
	ErrRateLimited            = errors.New("RATE_LIMITED")
)

type PasswordMetrics struct{ Hashes, Verifications, CapacityRejected, InvalidHashes uint64 }
type Passwords struct {
	memory                                   uint32
	iterations                               uint32
	slots                                    chan struct{}
	hashes, verifications, rejected, invalid atomic.Uint64
}

func NewPasswords() *Passwords { return newPasswords(65536, 3) }
func newPasswords(memory, iterations uint32) *Passwords {
	return &Passwords{memory: memory, iterations: iterations, slots: make(chan struct{}, 2)}
}
func (p *Passwords) Metrics() PasswordMetrics {
	return PasswordMetrics{p.hashes.Load(), p.verifications.Load(), p.rejected.Load(), p.invalid.Load()}
}
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || len(password) > 512 {
		return ErrInvalidArgument
	}
	n := utf8.RuneCountInString(password)
	if n < 12 || n > 128 {
		return ErrInvalidArgument
	}
	return nil
}
func NormalizeUsername(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) < 3 || len(name) > 32 {
		return "", ErrInvalidArgument
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return "", ErrInvalidArgument
		}
	}
	return strings.ToLower(name), nil
}
func (p *Passwords) acquire() bool {
	select {
	case p.slots <- struct{}{}:
		return true
	default:
		p.rejected.Add(1)
		return false
	}
}
func (p *Passwords) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	if !p.acquire() {
		return "", ErrRateLimited
	}
	defer func() { <-p.slots }()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, 1, 32)
	p.hashes.Add(1)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=1$%s$%s", p.memory, p.iterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func (p *Passwords) Verify(encoded, password string) (bool, error) {
	if err := ValidatePassword(password); err != nil {
		return false, nil
	}
	m, t, salt, want, ok := parseHash(encoded)
	if !ok {
		p.invalid.Add(1)
		log.Print("invalid stored password encoding")
		return false, nil
	}
	if !p.acquire() {
		return false, ErrRateLimited
	}
	defer func() { <-p.slots }()
	got := argon2.IDKey([]byte(password), salt, t, m, 1, 32)
	p.verifications.Add(1)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
func parseHash(encoded string) (uint32, uint32, []byte, []byte, bool) {
	if len(encoded) > 150 {
		return 0, 0, nil, nil, false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return 0, 0, nil, nil, false
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 || !strings.HasPrefix(params[0], "m=") || !strings.HasPrefix(params[1], "t=") || params[2] != "p=1" {
		return 0, 0, nil, nil, false
	}
	m, e1 := strconv.ParseUint(strings.TrimPrefix(params[0], "m="), 10, 32)
	t, e2 := strconv.ParseUint(strings.TrimPrefix(params[1], "t="), 10, 32)
	if e1 != nil || e2 != nil || m < 8 || m > 65536 || t < 1 || t > 3 {
		return 0, 0, nil, nil, false
	}
	salt, e1 := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	key, e2 := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(key) != 32 {
		return 0, 0, nil, nil, false
	}
	return uint32(m), uint32(t), salt, key, true
}

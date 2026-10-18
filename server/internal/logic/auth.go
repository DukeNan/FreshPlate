package logic

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"freshplate/server/internal/store"
)

type Service struct {
	Store     *store.Store
	Passwords *Passwords
	Now       func() time.Time
	dummyHash string
}

func New(st *store.Store) (*Service, error) { return newService(st, NewPasswords()) }
func newService(st *store.Store, p *Passwords) (*Service, error) {
	h, err := p.Hash("freshplate dummy credential never authenticates")
	if err != nil {
		return nil, err
	}
	return &Service{Store: st, Passwords: p, Now: time.Now, dummyHash: h}, nil
}
func HashToken(raw string) ([]byte, error) {
	if len(raw) != 43 {
		return nil, ErrInvalidArgument
	}
	token, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(token) != 32 {
		return nil, ErrInvalidArgument
	}
	sum := sha256.Sum256(token)
	return sum[:], nil
}
func (s *Service) Register(ctx context.Context, name, password string) (store.User, error) {
	name, err := NormalizeUsername(name)
	if err != nil {
		return store.User{}, err
	}
	h, err := s.Passwords.Hash(password)
	if err != nil {
		return store.User{}, err
	}
	return s.Store.CreateUser(ctx, name, h, "user", s.Now())
}
func (s *Service) Login(ctx context.Context, name, password string) (store.User, string, error) {
	name, err := NormalizeUsername(name)
	if err != nil {
		if _, verifyErr := s.Passwords.Verify(s.dummyHash, password); verifyErr != nil {
			return store.User{}, "", verifyErr
		}
		return store.User{}, "", ErrInvalidCredentials
	}
	u, err := s.Store.UserByName(ctx, name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.User{}, "", err
	}
	h := u.PasswordHash
	if errors.Is(err, sql.ErrNoRows) {
		h = s.dummyHash
	}
	ok, err := s.Passwords.Verify(h, password)
	if err != nil {
		return store.User{}, "", err
	}
	if !ok || u.ID == 0 || u.Status != "active" {
		return store.User{}, "", ErrInvalidCredentials
	}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		return store.User{}, "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(token)
	sum := sha256.Sum256(token)
	now := s.Now()
	u, err = s.Store.IssueSession(ctx, u, sum[:], now, now.Add(7*24*time.Hour))
	if err != nil {
		return store.User{}, "", err
	}
	return u, raw, nil
}
func (s *Service) ChangePassword(ctx context.Context, session store.Session, current, next string) error {
	if err := ValidatePassword(next); err != nil {
		return err
	}
	u, err := s.Store.UserByID(ctx, session.User.ID)
	if err != nil {
		return err
	}
	if _, _, _, _, ok := parseHash(u.PasswordHash); !ok {
		return errors.New("invalid stored password encoding")
	}
	ok, err := s.Passwords.Verify(u.PasswordHash, current)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCurrentPassword
	}
	if current == next {
		return ErrSamePassword
	}
	h, err := s.Passwords.Hash(next)
	if err != nil {
		return err
	}
	return s.Store.ChangePassword(ctx, session, u, h, s.Now())
}
func (s *Service) ResetPassword(ctx context.Context, actor store.Session, targetID int64, temp string) error {
	if actor.User.ID == targetID {
		return store.E(409, "SELF_ADMIN_CHANGE_FORBIDDEN", "Cannot reset your own password")
	}
	if err := ValidatePassword(temp); err != nil {
		return err
	}
	u, err := s.Store.UserByID(ctx, targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.E(404, "NOT_FOUND", "User does not exist")
	}
	if err != nil {
		return err
	}
	if _, _, _, _, ok := parseHash(u.PasswordHash); !ok {
		return errors.New("invalid stored password encoding")
	}
	same, err := s.Passwords.Verify(u.PasswordHash, temp)
	if err != nil {
		return err
	}
	if same {
		return ErrSamePassword
	}
	h, err := s.Passwords.Hash(temp)
	if err != nil {
		return err
	}
	return s.Store.AdminReset(ctx, actor, u, h, s.Now())
}
func (s *Service) InitAdmin(ctx context.Context, name, password string) (store.User, error) {
	name, err := NormalizeUsername(name)
	if err != nil {
		return store.User{}, err
	}
	h, err := s.Passwords.Hash(password)
	if err != nil {
		return store.User{}, err
	}
	return s.Store.InitAdmin(ctx, name, h, s.Now())
}
func (s *Service) RecoverAdmin(ctx context.Context, name, password string) error {
	name, err := NormalizeUsername(name)
	if err != nil {
		return err
	}
	if err = ValidatePassword(password); err != nil {
		return err
	}
	u, err := s.Store.UserByName(ctx, name)
	if err != nil {
		return err
	}
	if u.Role != "admin" || u.Status != "active" {
		return store.E(403, "FORBIDDEN", "Only an active administrator can be recovered")
	}
	if _, _, _, _, ok := parseHash(u.PasswordHash); !ok {
		return errors.New("invalid stored password encoding")
	}
	same, err := s.Passwords.Verify(u.PasswordHash, password)
	if err != nil {
		return err
	}
	if same {
		return ErrSamePassword
	}
	h, err := s.Passwords.Hash(password)
	if err != nil {
		return err
	}
	return s.Store.RecoverAdmin(ctx, u, h, s.Now())
}

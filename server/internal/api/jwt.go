package api

import (
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"freshplate/server/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type accessClaims struct {
	SID string `json:"sid"`
	jwt.RegisteredClaims
}

func (a *API) access(s *store.Session) (string, int, error) {
	now := a.service.Now()
	end := now.Add(15 * time.Minute)
	if s.ExpiresAt.Before(end) {
		end = s.ExpiresAt
	}
	claims := accessClaims{hex.EncodeToString(s.TokenHash), jwt.RegisteredClaims{
		Issuer: "freshplate", Audience: jwt.ClaimStrings{"freshplate-api"}, Subject: strconv.FormatInt(s.User.ID, 10),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(end),
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.config.JWTKey)
	return token, int(end.Unix() - now.Unix()), err
}
func (a *API) authenticate(c *gin.Context, raw string) error {
	claims := new(accessClaims)
	_, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return a.config.JWTKey, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("freshplate"), jwt.WithAudience("freshplate-api"),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(a.service.Now))
	// Reject all malformed claims before distinguishing an otherwise valid expired token.
	if err != nil && !errors.Is(err, jwt.ErrTokenExpired) {
		return store.E(401, "AUTH_REQUIRED", "请重新登录")
	}
	if err != nil && !onlyExpired(err) {
		return store.E(401, "AUTH_REQUIRED", "请重新登录")
	}
	sid, e := hex.DecodeString(claims.SID)
	id, eID := strconv.ParseInt(claims.Subject, 10, 64)
	if e != nil || len(sid) != 32 || hex.EncodeToString(sid) != claims.SID || eID != nil || id <= 0 || strconv.FormatInt(id, 10) != claims.Subject || claims.IssuedAt == nil || claims.ExpiresAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) || claims.ExpiresAt.Sub(claims.IssuedAt.Time) > 15*time.Minute {
		return store.E(401, "AUTH_REQUIRED", "请重新登录")
	}
	s, e := a.service.Store.ResolveSession(c.Request.Context(), sid, a.service.Now())
	if e != nil {
		return e
	}
	if s == nil || s.User.ID != id {
		return store.E(401, "AUTH_REQUIRED", "请重新登录")
	}
	if err != nil {
		return store.E(401, "ACCESS_TOKEN_EXPIRED", "访问令牌已过期")
	}
	c.Set("session", s)
	return nil
}
func onlyExpired(err error) bool {
	// jwt wraps validation failures in joined errors; remove only the expected wrappers.
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range multi.Unwrap() {
			if !onlyExpired(e) {
				return false
			}
		}
		return true
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		return onlyExpired(single.Unwrap())
	}
	return err == jwt.ErrTokenExpired || err == jwt.ErrTokenInvalidClaims
}

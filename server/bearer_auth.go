package chserver

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"

	"github.com/cloudradar-monitoring/rport/server/api/session"
)

const (
	maxTokenLifetime     = 90 * 24 * time.Hour
	defaultTokenLifetime = 10 * time.Minute
)

type Token struct {
	Username string `json:"username,omitempty"`
	jwt.StandardClaims
}

func (al *APIListener) createAuthToken(ctx context.Context, lifetime time.Duration, username string) (string, error) {
	if username == "" {
		return "", errors.New("username cannot be empty")
	}

	claims := Token{
		Username: username,
		StandardClaims: jwt.StandardClaims{
			Id: strconv.FormatUint(rand.Uint64(), 10),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(al.config.API.JWTSecret))
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(lifetime)
	err = al.apiSessions.Save(ctx, &session.APISession{Token: tokenStr, ExpiresAt: expiresAt})
	if err != nil {
		return "", err
	}

	return tokenStr, nil
}

func (al *APIListener) increaseSessionLifetime(ctx context.Context, s *session.APISession) error {
	s.ExpiresAt = time.Now().Add(defaultTokenLifetime)
	return al.apiSessions.Save(ctx, s)
}

func (al *APIListener) validateBearerToken(ctx context.Context, tokenStr string) (bool, string, *session.APISession, error) {
	tk := &Token{}
	token, err := jwt.ParseWithClaims(tokenStr, tk, func(token *jwt.Token) (i interface{}, err error) {
		return []byte(al.config.API.JWTSecret), nil
	})
	if err != nil {
		// do not return error since it should respond with 401 instead of 500, just log it
		al.Debugf("failed to parse jwt token: %v", err)
		return false, "", nil, nil
	}

	if al.bannedUsers.IsBanned(tk.Username) {
		return false, tk.Username, nil, ErrTooManyRequests
	}

	if !token.Valid || tk.Username == "" {
		return false, "", nil, nil
	}

	apiSession, err := al.apiSessions.Get(ctx, tokenStr)
	if err != nil || apiSession == nil {
		return false, "", nil, err
	}

	return apiSession.ExpiresAt.After(time.Now()), tk.Username, apiSession, nil
}

func getBearerToken(req *http.Request) (string, bool) {
	auth := req.Header.Get("Authorization")
	const prefix = "Bearer "
	// Case insensitive prefix match.
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", false
	}
	return auth[len(prefix):], true
}

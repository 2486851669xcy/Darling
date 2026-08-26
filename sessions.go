package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	sessionTokenBytes = 32
	defaultSessionTTL = 30 * 24 * time.Hour
)

type PostgresSessionStore struct {
	DB  *sql.DB
	TTL time.Duration
}

func (s PostgresSessionStore) Resolve(ctx context.Context, token string) (string, bool, error) {
	if ctx == nil {
		return "", false, errors.New("resolve session: context must not be nil")
	}
	if s.DB == nil {
		return "", false, errors.New("resolve session: database must not be nil")
	}
	normalized, ok := normalizeSessionToken(token)
	if !ok {
		return "", false, nil
	}
	hash, err := hashSessionToken(normalized)
	if err != nil {
		return "", false, err
	}
	var userID string
	err = s.DB.QueryRowContext(ctx, "SELECT replace(user_id::text, '-', '') FROM user_sessions WHERE token_hash = $1 AND expires_at > CURRENT_TIMESTAMP", hash).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve session: %w", err)
	}
	if _, valid := normalizeUserID(userID); !valid {
		return "", false, errors.New("resolve session: database returned invalid user id")
	}
	return userID, true, nil
}

func (s PostgresSessionStore) Issue(ctx context.Context, userID string) (string, error) {
	if ctx == nil {
		return "", errors.New("issue session: context must not be nil")
	}
	if s.DB == nil {
		return "", errors.New("issue session: database must not be nil")
	}
	userUUID, err := normalizePostgresUserUUID(userID)
	if err != nil {
		return "", err
	}
	return issueSessionWithExecutor(ctx, s.DB, userUUID, s.sessionTTL())
}

func (s PostgresSessionStore) Rotate(ctx context.Context, oldToken, targetUserID string) (string, error) {
	if ctx == nil {
		return "", errors.New("rotate session: context must not be nil")
	}
	if s.DB == nil {
		return "", errors.New("rotate session: database must not be nil")
	}
	userUUID, err := normalizePostgresUserUUID(targetUserID)
	if err != nil {
		return "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("rotate session: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	newToken, err := issueSessionWithExecutor(ctx, tx, userUUID, s.sessionTTL())
	if err != nil {
		return "", err
	}
	if normalized, ok := normalizeSessionToken(oldToken); ok {
		hash, err := hashSessionToken(normalized)
		if err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE token_hash = $1", hash); err != nil {
			return "", fmt.Errorf("rotate session: revoke old token: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("rotate session: commit: %w", err)
	}
	return newToken, nil
}

func (s PostgresSessionStore) Revoke(ctx context.Context, token string) error {
	if ctx == nil {
		return errors.New("revoke session: context must not be nil")
	}
	if s.DB == nil {
		return errors.New("revoke session: database must not be nil")
	}
	normalized, ok := normalizeSessionToken(token)
	if !ok {
		return nil
	}
	hash, err := hashSessionToken(normalized)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "DELETE FROM user_sessions WHERE token_hash = $1", hash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s PostgresSessionStore) sessionTTL() time.Duration {
	if s.TTL <= 0 {
		return defaultSessionTTL
	}
	return s.TTL
}

type sessionExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func issueSessionWithExecutor(ctx context.Context, executor sessionExecutor, userUUID string, ttl time.Duration) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		token, err := newSessionToken()
		if err != nil {
			return "", err
		}
		hash, err := hashSessionToken(token)
		if err != nil {
			return "", err
		}
		_, err = executor.ExecContext(ctx, "INSERT INTO user_sessions(token_hash, user_id, expires_at) VALUES ($1, $2::uuid, $3)", hash, userUUID, time.Now().UTC().Add(ttl))
		if err == nil {
			return token, nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return "", fmt.Errorf("issue session: %w", err)
		}
	}
	return "", errors.New("issue session: token collision limit reached")
}

func newSessionToken() (string, error) {
	value := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func normalizeSessionToken(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sessionTokenBytes*2 {
		return "", false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sessionTokenBytes {
		return "", false
	}
	return value, true
}

func hashSessionToken(token string) ([]byte, error) {
	normalized, ok := normalizeSessionToken(token)
	if !ok {
		return nil, errors.New("invalid session token")
	}
	sum := sha256.Sum256([]byte(normalized))
	return sum[:], nil
}

func (a *App) rotateUserSession(c *gin.Context, targetUserID string) error {
	if a == nil {
		return errors.New("rotate user session: app is nil")
	}
	newToken, err := a.sessions.Rotate(c.Request.Context(), readUserCookie(c), targetUserID)
	if err != nil {
		return err
	}
	writeUserCookie(c, newToken)
	return nil
}

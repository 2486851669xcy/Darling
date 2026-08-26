package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewSessionTokenIs256BitHex(t *testing.T) {
	seen := make(map[string]bool, 32)
	for i := 0; i < 32; i++ {
		token, err := newSessionToken()
		if err != nil {
			t.Fatalf("newSessionToken: %v", err)
		}
		decoded, err := hex.DecodeString(token)
		if err != nil || len(token) != 64 || len(decoded) != 32 {
			t.Fatalf("token is not 256-bit hex: len=%d decoded=%d err=%v", len(token), len(decoded), err)
		}
		if seen[token] {
			t.Fatalf("duplicate token at iteration %d", i)
		}
		seen[token] = true
	}
}

func TestNormalizeAndHashSessionToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	normalized, ok := normalizeSessionToken(" \t" + strings.ToUpper(token) + "\r\n")
	if !ok || normalized != token {
		t.Fatalf("normalize = (%q, %v), want (%q, true)", normalized, ok, token)
	}

	got, err := hashSessionToken(strings.ToUpper(token))
	if err != nil {
		t.Fatalf("hash token: %v", err)
	}
	want := sha256.Sum256([]byte(token))
	if !bytes.Equal(got, want[:]) {
		t.Fatalf("hash = %x, want %x", got, want)
	}
	assertDoesNotContainRawToken(t, got, token)

	for _, invalid := range []string{"", strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("g", 64), token + " secret"} {
		if got, ok := normalizeSessionToken(invalid); ok || got != "" {
			t.Errorf("normalize invalid token = (%q, %v)", got, ok)
		}
		if _, err := hashSessionToken(invalid); err == nil {
			t.Errorf("hashSessionToken(%q) succeeded", invalid)
		} else if invalid != "" && strings.Contains(err.Error(), invalid) {
			t.Errorf("hash error leaked token: %v", err)
		}
	}
}

func TestIssueSessionStoresOnlyHash(t *testing.T) {
	const userUUID = "01234567-89ab-cdef-0123-456789abcdef"
	executor := &recordingSessionExecutor{}
	token, err := issueSessionWithExecutor(context.Background(), executor, userUUID, time.Hour)
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	if executor.calls != 1 || len(executor.args) != 3 {
		t.Fatalf("executor calls=%d args=%d, want 1 and 3", executor.calls, len(executor.args))
	}
	if strings.Contains(executor.query, token) {
		t.Fatal("INSERT query leaked raw token")
	}
	storedHash, ok := executor.args[0].([]byte)
	if !ok {
		t.Fatalf("hash arg type = %T, want []byte", executor.args[0])
	}
	wantHash, _ := hashSessionToken(token)
	if !bytes.Equal(storedHash, wantHash) {
		t.Fatalf("stored hash = %x, want %x", storedHash, wantHash)
	}
	assertDoesNotContainRawToken(t, storedHash, token)
	if executor.args[1] != userUUID {
		t.Fatalf("stored user = %#v, want %q", executor.args[1], userUUID)
	}
	if _, ok := executor.args[2].(time.Time); !ok {
		t.Fatalf("expiry arg type = %T, want time.Time", executor.args[2])
	}
}

func TestPostgresSessionLifecycleIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := OpenPostgres(ctx, databaseURL, 4, 2)
	if err != nil {
		t.Fatalf("open test PostgreSQL: %v", err)
	}
	defer func() { _ = db.Close() }()

	userA, userB := newPostgresTestUserID(t), newPostgresTestUserID(t)
	userAUUID, _ := normalizePostgresUserUUID(userA)
	userBUUID, _ := normalizePostgresUserUUID(userB)
	if _, err := db.ExecContext(ctx, `INSERT INTO users(user_id) VALUES ($1::uuid), ($2::uuid)`, userAUUID, userBUUID); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM users WHERE user_id IN ($1::uuid, $2::uuid)`, userAUUID, userBUUID)
	}()

	store := PostgresSessionStore{DB: db, TTL: time.Hour}
	token, err := store.Issue(ctx, userA)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	assertCanonicalSessionToken(t, token)
	wantHash, _ := hashSessionToken(token)
	var storedHash []byte
	if err := db.QueryRowContext(ctx, `SELECT token_hash FROM user_sessions WHERE user_id = $1::uuid`, userAUUID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored hash: %v", err)
	}
	if !bytes.Equal(storedHash, wantHash) {
		t.Fatalf("stored hash = %x, want %x", storedHash, wantHash)
	}
	assertDoesNotContainRawToken(t, storedHash, token)

	resolved, found, err := store.Resolve(ctx, " \t"+strings.ToUpper(token)+"\n")
	if err != nil || !found || resolved != userA {
		t.Fatalf("Resolve = (%q, %v, %v), want (%q, true, nil)", resolved, found, err, userA)
	}
	if resolved, found, err := store.Resolve(ctx, "invalid"); err != nil || found || resolved != "" {
		t.Fatalf("Resolve invalid = (%q, %v, %v)", resolved, found, err)
	}

	rotated, err := store.Rotate(ctx, token, userB)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	assertCanonicalSessionToken(t, rotated)
	if rotated == token {
		t.Fatal("Rotate returned old token")
	}
	if resolved, found, err := store.Resolve(ctx, token); err != nil || found || resolved != "" {
		t.Fatalf("old token still resolves: (%q, %v, %v)", resolved, found, err)
	}
	if resolved, found, err := store.Resolve(ctx, rotated); err != nil || !found || resolved != userB {
		t.Fatalf("rotated Resolve = (%q, %v, %v), want (%q, true, nil)", resolved, found, err, userB)
	}

	if err := store.Revoke(ctx, rotated); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := store.Revoke(ctx, rotated); err != nil {
		t.Fatalf("idempotent Revoke: %v", err)
	}
	if resolved, found, err := store.Resolve(ctx, rotated); err != nil || found || resolved != "" {
		t.Fatalf("revoked token resolves: (%q, %v, %v)", resolved, found, err)
	}
}

func assertCanonicalSessionToken(t *testing.T, token string) {
	t.Helper()
	if normalized, ok := normalizeSessionToken(token); !ok || normalized != token {
		t.Fatal("issued token is not canonical 256-bit hex")
	}
}

func assertDoesNotContainRawToken(t *testing.T, value []byte, token string) {
	t.Helper()
	canonical, _ := normalizeSessionToken(token)
	if bytes.Equal(value, []byte(token)) ||
		bytes.Contains(value, []byte(token)) ||
		hex.EncodeToString(value) == canonical {
		t.Fatal("raw bearer token leaked into stored hash")
	}
}

type recordingSessionExecutor struct {
	calls int
	query string
	args  []any
}

func (e *recordingSessionExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.calls++
	e.query = query
	e.args = append([]any(nil), args...)
	return driver.RowsAffected(1), nil
}

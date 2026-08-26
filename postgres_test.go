package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNormalizePostgresUserUUID(t *testing.T) {
	const cookieID = "ABCDEF0123456789ABCDEF0123456789"
	got, err := normalizePostgresUserUUID(cookieID)
	if err != nil {
		t.Fatalf("normalize cookie user id: %v", err)
	}
	const want = "abcdef01-2345-6789-abcd-ef0123456789"
	if got != want {
		t.Fatalf("normalized UUID = %q, want %q", got, want)
	}

	for _, invalid := range []string{
		"",
		"../escape",
		"abcdef01-2345-6789-abcd-ef0123456789",
		strings.Repeat("f", 31),
		strings.Repeat("f", 33),
		strings.Repeat("g", 32),
	} {
		if _, err := normalizePostgresUserUUID(invalid); err == nil {
			t.Errorf("normalizePostgresUserUUID(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestPostgresSchemaIsTenantScoped(t *testing.T) {
	schema := strings.ToUpper(strings.Join(postgresSchemaStatements, "\n"))
	if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS USERS") ||
		!strings.Contains(schema, "USER_ID UUID PRIMARY KEY") {
		t.Fatal("users table does not use a UUID tenant key")
	}
	for _, table := range []string{
		"MESSAGES",
		"STICKER_ASSETS",
		"MOMENTS",
		"MOMENT_LIKES",
		"MOMENT_COMMENTS",
		"MOMENT_CHECKS",
	} {
		definition := postgresCreateTableStatement(t, table)
		if !strings.Contains(definition, "USER_ID UUID NOT NULL") {
			t.Errorf("%s table is missing a required UUID user_id", table)
		}
		if !strings.Contains(definition, "TIMESTAMPTZ") {
			t.Errorf("%s table is missing TIMESTAMPTZ timestamps", table)
		}
	}

	for _, table := range []string{"MOMENT_LIKES", "MOMENT_COMMENTS", "MOMENT_CHECKS"} {
		definition := postgresCreateTableStatement(t, table)
		if !strings.Contains(definition, "FOREIGN KEY (USER_ID, MOMENT_ID)") ||
			!strings.Contains(definition, "REFERENCES MOMENTS(USER_ID, ID)") {
			t.Errorf("%s does not enforce a composite tenant foreign key", table)
		}
	}

	if strings.Contains(schema, "AUTOINCREMENT") || strings.Contains(schema, " DATETIME") {
		t.Fatal("PostgreSQL schema contains SQLite-only types or identity syntax")
	}
}

func TestPostgresIndexesLeadWithUserIDAndDoNotIndexStickerURL(t *testing.T) {
	indexCount := 0
	for _, statement := range postgresSchemaStatements {
		upper := strings.ToUpper(strings.TrimSpace(statement))
		if !strings.HasPrefix(upper, "CREATE INDEX") {
			continue
		}
		indexCount++
		if strings.Contains(upper, "URL") {
			t.Errorf("long sticker URL must not be indexed: %s", statement)
		}
		on := strings.Index(upper, " ON ")
		if on < 0 {
			t.Errorf("cannot inspect index statement: %s", statement)
			continue
		}
		indexDefinition := upper[on+4:]
		open := strings.Index(indexDefinition, "(")
		if open < 0 {
			t.Errorf("cannot inspect index columns: %s", statement)
			continue
		}
		columns := strings.TrimSpace(indexDefinition[open+1:])
		if !strings.HasPrefix(columns, "USER_ID,") {
			t.Errorf("tenant index must lead with user_id: %s", statement)
		}
	}
	if indexCount == 0 {
		t.Fatal("schema did not define any tenant indexes")
	}
}

func TestOpenPostgresRejectsInvalidConfigurationWithoutConnecting(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		url     string
		maxOpen int
		maxIdle int
	}{
		{name: "empty URL", url: "", maxOpen: 4, maxIdle: 2},
		{name: "zero max open", url: "postgres://invalid", maxOpen: 0, maxIdle: 0},
		{name: "negative max idle", url: "postgres://invalid", maxOpen: 4, maxIdle: -1},
		{name: "idle exceeds open", url: "postgres://invalid", maxOpen: 4, maxIdle: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := OpenPostgres(ctx, test.url, test.maxOpen, test.maxIdle)
			if err == nil || db != nil {
				t.Fatalf("OpenPostgres returned db=%v, error=%v", db, err)
			}
		})
	}
}

func TestPostgresSchemaAndUserStoreIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := OpenPostgres(ctx, databaseURL, 4, 2)
	if err != nil {
		t.Fatalf("open test PostgreSQL database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := initPostgresSchema(ctx, db); err != nil {
		t.Fatalf("second schema initialization was not idempotent: %v", err)
	}

	userA := newPostgresTestUserID(t)
	userB := newPostgresTestUserID(t)
	userOverLimit := newPostgresTestUserID(t)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `
DELETE FROM users
WHERE user_id IN ($1::uuid, $2::uuid, $3::uuid)`, userA, userB, userOverLimit)
	}()

	var existingUsers int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&existingUsers); err != nil {
		t.Fatalf("count existing test users: %v", err)
	}
	store := PostgresUserStore{DB: db, MaxUsers: existingUsers + 2}

	created, err := store.EnsureUser(ctx, userA)
	if err != nil || !created {
		t.Fatalf("ensure first PostgreSQL user returned created=%v, error=%v", created, err)
	}
	exists, err := store.UserExists(ctx, userA)
	if err != nil || !exists {
		t.Fatalf("first PostgreSQL user exists=%v, error=%v", exists, err)
	}
	created, err = store.EnsureUser(ctx, userA)
	if err != nil || created {
		t.Fatalf("ensure existing PostgreSQL user returned created=%v, error=%v", created, err)
	}
	created, err = store.EnsureUser(ctx, userB)
	if err != nil || !created {
		t.Fatalf("ensure second PostgreSQL user returned created=%v, error=%v", created, err)
	}
	created, err = store.EnsureUser(ctx, userOverLimit)
	if !errors.Is(err, errUserLimitReached) || created {
		t.Fatalf("ensure over-limit user returned created=%v, error=%v", created, err)
	}

	var momentID int64
	if err := db.QueryRowContext(ctx, `
INSERT INTO moments(user_id, character_id, author, content, image_url)
VALUES ($1::uuid, 'luna', 'user', 'tenant-a-moment', '')
RETURNING id`, userA).Scan(&momentID); err != nil {
		t.Fatalf("insert tenant A moment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO moment_comments(user_id, moment_id, author, content)
VALUES ($1::uuid, $2, 'character', 'cross-tenant-comment')`, userB, momentID); err == nil {
		t.Fatal("composite tenant foreign key allowed a cross-user moment comment")
	}
}

func postgresCreateTableStatement(t *testing.T, table string) string {
	t.Helper()
	prefix := "CREATE TABLE IF NOT EXISTS " + strings.ToUpper(table) + " "
	for _, statement := range postgresSchemaStatements {
		upper := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(upper, prefix) {
			return upper
		}
	}
	t.Fatalf("schema does not define table %s", table)
	return ""
}

func newPostgresTestUserID(t *testing.T) string {
	t.Helper()
	value := make([]byte, 16)
	if _, err := cryptorand.Read(value); err != nil {
		t.Fatalf("generate PostgreSQL test user id: %v", err)
	}
	return hex.EncodeToString(value)
}

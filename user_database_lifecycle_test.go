package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresStorageIsolatesAllUserData(t *testing.T) {
	db := openPostgresIntegrationTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var existingUsers int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&existingUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}
	store := PostgresUserStore{DB: db, MaxUsers: existingUsers + 10}
	userA := newPostgresTestUserID(t)
	userB := newPostgresTestUserID(t)
	for _, userID := range []string{userA, userB} {
		created, err := store.EnsureUser(ctx, userID)
		if err != nil || !created {
			t.Fatalf("ensure user %s: created=%v error=%v", userID, created, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE user_id IN ($1::uuid, $2::uuid)", userA, userB)
	})

	app := &App{db: db}
	ctxA := contextWithUserSession(ctx, UserSession{ID: userA})
	ctxB := contextWithUserSession(ctx, UserSession{ID: userB})

	messageA, err := app.saveMessage(ctxA, "luna", "user", "text", "message-only-a")
	if err != nil {
		t.Fatalf("save user A message: %v", err)
	}
	if _, err := app.saveMessage(ctxB, "luna", "user", "text", "message-only-b"); err != nil {
		t.Fatalf("save user B message: %v", err)
	}
	if _, err := app.getMessageByID(ctxB, messageA.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("user B read user A message: %v", err)
	}
	assertOnlyMessageContent(t, app, ctxA, "message-only-a")
	assertOnlyMessageContent(t, app, ctxB, "message-only-b")

	momentA, err := app.saveMoment(ctxA, "luna", "user", "moment-only-a", "")
	if err != nil {
		t.Fatalf("save user A moment: %v", err)
	}
	if _, err := app.saveMoment(ctxB, "luna", "user", "moment-only-b", ""); err != nil {
		t.Fatalf("save user B moment: %v", err)
	}
	if _, _, err := app.saveMomentLike(ctxA, momentA.ID, "character"); err != nil {
		t.Fatalf("like user A moment: %v", err)
	}
	if _, err := app.saveMomentComment(ctxA, momentA.ID, "character", "private-comment-a"); err != nil {
		t.Fatalf("comment user A moment: %v", err)
	}
	if err := app.saveMomentCheck(ctxA, momentA.ID, "character", "comment"); err != nil {
		t.Fatalf("check user A moment: %v", err)
	}
	if _, err := app.saveMomentComment(ctxB, momentA.ID, "character", "cross-tenant"); err == nil {
		t.Fatal("cross-tenant moment comment unexpectedly succeeded")
	}
	if err := app.saveMomentCheck(ctxB, momentA.ID, "character", "seen"); err == nil {
		t.Fatal("cross-tenant moment check unexpectedly succeeded")
	}

	if err := app.saveStickerAsset(ctxA, "luna", "happy", "https://example.test/a.webp", "test", ""); err != nil {
		t.Fatalf("save user A sticker: %v", err)
	}
	if got := app.getGeneratedStickerCandidates(ctxB, "luna", "happy"); len(got) != 0 {
		t.Fatalf("user B saw user A sticker assets: %v", got)
	}

	momentsA, err := app.getMoments(ctxA, "luna", 10)
	if err != nil {
		t.Fatalf("get user A moments: %v", err)
	}
	if len(momentsA) != 1 || momentsA[0].Content != "moment-only-a" ||
		len(momentsA[0].Likes) != 1 || len(momentsA[0].Comments) != 1 ||
		!app.hasMomentCheck(ctxA, momentA.ID, "character") {
		t.Fatalf("unexpected user A moment graph: %#v", momentsA)
	}
	momentsB, err := app.getMoments(ctxB, "luna", 10)
	if err != nil {
		t.Fatalf("get user B moments: %v", err)
	}
	if len(momentsB) != 1 || momentsB[0].Content != "moment-only-b" {
		t.Fatalf("unexpected user B moments: %#v", momentsB)
	}
}

func openPostgresIntegrationTest(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := OpenPostgres(ctx, databaseURL, 8, 2)
	if err != nil {
		t.Fatalf("open PostgreSQL integration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertOnlyMessageContent(t *testing.T, app *App, ctx context.Context, want string) {
	t.Helper()
	messages, err := app.getMessages(ctx, "luna", 20)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != want {
		t.Fatalf("message contents=%v, want only %q", messages, want)
	}
}

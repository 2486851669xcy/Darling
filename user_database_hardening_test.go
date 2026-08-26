package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestPostgresSessionMiddlewareCreatesReusesAndReplacesIdentity(t *testing.T) {
	db := openPostgresIntegrationTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var existingUsers int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&existingUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}
	app := &App{
		db:       db,
		users:    PostgresUserStore{DB: db, MaxUsers: existingUsers + 10},
		sessions: PostgresSessionStore{DB: db, TTL: time.Hour},
	}
	router := newPostgresSessionRouter(app)

	first := performSessionRequest(router, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first session status=%d body=%s", first.Code, first.Body.String())
	}
	cookie := responseCookieByName(t, first, userCookieName)
	assertSafeSessionBody(t, first)
	resolvedUserID, resolved, err := app.sessions.Resolve(ctx, cookie.Value)
	if err != nil || !resolved {
		t.Fatalf("resolve created session user=%q resolved=%v error=%v", resolvedUserID, resolved, err)
	}
	exists, err := app.users.UserExists(ctx, resolvedUserID)
	if err != nil || !exists {
		t.Fatalf("created user exists=%v error=%v", exists, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		normalized, _ := normalizePostgresUserUUID(resolvedUserID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE user_id = $1::uuid", normalized)
	})

	second := performSessionRequest(router, cookie)
	if second.Code != http.StatusOK {
		t.Fatalf("reused session status=%d body=%s", second.Code, second.Body.String())
	}
	if replacement := optionalCookieByName(second, userCookieName); replacement != nil {
		t.Fatalf("known session cookie was replaced: %#v", replacement)
	}

	rawReplayResponse := performSessionRequest(router, &http.Cookie{Name: userCookieName, Value: resolvedUserID})
	if rawReplayResponse.Code != http.StatusOK {
		t.Fatalf("raw user id replay status=%d body=%s", rawReplayResponse.Code, rawReplayResponse.Body.String())
	}
	rawReplayToken := responseCookieByName(t, rawReplayResponse, userCookieName)
	if _, valid := normalizeSessionToken(rawReplayToken.Value); !valid {
		t.Fatal("raw user id replay did not receive an unrelated opaque session")
	}
	rawReplayUserID, resolved, err := app.sessions.Resolve(ctx, rawReplayToken.Value)
	if err != nil || !resolved {
		t.Fatalf("resolve raw replay session user=%q resolved=%v error=%v", rawReplayUserID, resolved, err)
	}
	if rawReplayUserID == resolvedUserID {
		t.Fatal("raw user id replay authenticated as the original user")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		normalized, _ := normalizePostgresUserUUID(rawReplayUserID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE user_id = $1::uuid", normalized)
	})

	unknown := &http.Cookie{Name: userCookieName, Value: strings.Repeat("d", userIDBytes*2)}
	replaced := performSessionRequest(router, unknown)
	if replaced.Code != http.StatusOK {
		t.Fatalf("unknown session status=%d body=%s", replaced.Code, replaced.Body.String())
	}
	replacement := responseCookieByName(t, replaced, userCookieName)
	if replacement.Value == unknown.Value {
		t.Fatal("well-formed unknown cookie was accepted")
	}
	if _, valid := normalizeSessionToken(replacement.Value); !valid {
		t.Fatalf("replacement cookie is not an opaque session token")
	}
	assertSafeSessionBody(t, replaced)
	replacementUserID, resolved, err := app.sessions.Resolve(ctx, replacement.Value)
	if err != nil || !resolved {
		t.Fatalf("resolve replacement session user=%q resolved=%v error=%v", replacementUserID, resolved, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		normalized, _ := normalizePostgresUserUUID(replacementUserID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE user_id = $1::uuid", normalized)
	})
}

func newPostgresSessionRouter(app *App) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(app.userSessionMiddleware())
	router.GET("/api/session", app.handleGetSession)
	return router
}

func performSessionRequest(handler http.Handler, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func responseCookieByName(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	cookie := optionalCookieByName(response, name)
	if cookie == nil {
		t.Fatalf("response did not set cookie %q", name)
	}
	return cookie
}

func optionalCookieByName(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func assertSafeSessionBody(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("session payload=%v", payload)
	}
	if _, exposed := payload["user_id"]; exposed {
		t.Fatalf("session response exposed HttpOnly identity: %v", payload)
	}
}

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizeUserID(t *testing.T) {
	valid := strings.Repeat("a", userIDBytes*2)
	if got, ok := normalizeUserID(strings.ToUpper(valid)); !ok || got != valid {
		t.Fatalf("normalizeUserID returned %q, %v", got, ok)
	}
	for _, value := range []string{"", "../escape", strings.Repeat("a", 31), strings.Repeat("g", 32)} {
		if _, ok := normalizeUserID(value); ok {
			t.Fatalf("normalizeUserID(%q) unexpectedly succeeded", value)
		}
	}
}

func TestUserSessionContextCanBeRetained(t *testing.T) {
	userID := strings.Repeat("b", userIDBytes*2)
	ctx := contextWithUserSession(context.Background(), UserSession{ID: userID})
	session, err := userSessionFromContext(ctx)
	if err != nil || session.ID != userID {
		t.Fatalf("read session ID=%q, error=%v", session.ID, err)
	}
	retained, release, err := retainedUserContext(ctx)
	if err != nil {
		t.Fatalf("retain user context: %v", err)
	}
	release()
	retainedSession, err := userSessionFromContext(retained)
	if err != nil || retainedSession.ID != userID {
		t.Fatalf("retained session ID=%q, error=%v", retainedSession.ID, err)
	}
}

func TestWriteUserCookieSecurityAttributes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://example.test/api/session", nil)
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	sessionToken := strings.Repeat("c", sessionTokenBytes*2)
	writeUserCookie(context, sessionToken)

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count=%d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != userCookieName || cookie.Value != sessionToken {
		t.Fatalf("unexpected cookie identity: %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("insecure cookie attributes: %#v", cookie)
	}
}

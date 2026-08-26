package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	userCookieName          = "darling_user"
	userIDBytes             = 16
	userCookieAge           = defaultSessionTTL
	defaultMaxPostgresUsers = 1000
)

var errUserSessionMissing = errors.New("user session is missing from request context")

type userSessionContextKey struct{}

type UserSession struct {
	ID string
}

func pathIsWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

func normalizeUserID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) != userIDBytes*2 {
		return "", false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != userIDBytes {
		return "", false
	}
	return strings.ToLower(value), true
}

func newUserID() (string, error) {
	value := make([]byte, userIDBytes)
	if _, err := cryptorand.Read(value); err != nil {
		return "", fmt.Errorf("generate user id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func contextWithUserSession(ctx context.Context, session UserSession) context.Context {
	return context.WithValue(ctx, userSessionContextKey{}, session)
}

func userSessionFromContext(ctx context.Context) (UserSession, error) {
	if ctx == nil {
		return UserSession{}, errUserSessionMissing
	}
	session, ok := ctx.Value(userSessionContextKey{}).(UserSession)
	if !ok {
		return UserSession{}, errUserSessionMissing
	}
	if _, valid := normalizeUserID(session.ID); !valid {
		return UserSession{}, errUserSessionMissing
	}
	return session, nil
}

func retainedUserContext(ctx context.Context) (context.Context, func(), error) {
	session, err := userSessionFromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	backgroundSession := UserSession{ID: session.ID}
	return contextWithUserSession(context.Background(), backgroundSession), func() {}, nil
}

func (a *App) userSessionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.beginRequest() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "server is shutting down"})
			return
		}
		defer a.endRequest()

		ctx := c.Request.Context()
		cookieValue := readUserCookie(c)
		userID, valid, err := a.sessions.Resolve(ctx, cookieValue)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "check user session failed"})
			return
		}

		needsToken := !valid
		if !valid {
			userID, err = a.createAnonymousUser(ctx)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, errUserLimitReached) {
					status = http.StatusServiceUnavailable
				}
				c.AbortWithStatusJSON(status, gin.H{"error": "create user session failed"})
				return
			}
		}
		if needsToken {
			token, err := a.sessions.Issue(ctx, userID)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "issue user session failed"})
				return
			}
			writeUserCookie(c, token)
		}

		session := UserSession{ID: userID}
		c.Request = c.Request.WithContext(contextWithUserSession(ctx, session))
		c.Header("Cache-Control", "no-store")
		c.Writer.Header().Add("Vary", "Cookie")
		c.Next()
	}
}

func (a *App) createAnonymousUser(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		generatedID, err := newUserID()
		if err != nil {
			return "", err
		}
		created, err := a.users.EnsureUser(ctx, generatedID)
		if err != nil {
			return "", err
		}
		if created {
			return generatedID, nil
		}
	}
	return "", errors.New("create unique user session failed")
}

func readUserCookie(c *gin.Context) string {
	value, err := c.Cookie(userCookieName)
	if err != nil {
		return ""
	}
	return value
}

func writeUserCookie(c *gin.Context, sessionToken string) {
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     userCookieName,
		Value:    sessionToken,
		Path:     "/",
		Expires:  time.Now().Add(userCookieAge),
		MaxAge:   int(userCookieAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearUserCookie(c *gin.Context) {
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     userCookieName,
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) handleLogout(c *gin.Context) {
	if err := a.sessions.Revoke(c.Request.Context(), readUserCookie(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "logout failed"})
		return
	}
	clearUserCookie(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleGetSession(c *gin.Context) {
	if a.wechatAuth != nil {
		a.wechatAuth.SessionHandler(a)(c)
		return
	}
	if _, err := userSessionFromContext(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user session unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "authenticated": false, "wechat_enabled": false})
}

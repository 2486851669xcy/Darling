package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSanitizedRecoveryDoesNotLogOAuthQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	router := gin.New()
	router.Use(sanitizedRecoveryMiddleware())
	router.GET("/api/auth/wechat/callback", func(*gin.Context) {
		panic("provider failure")
	})

	request := httptest.NewRequest(http.MethodGet, "/api/auth/wechat/callback?code=secret-code&state=secret-state", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", response.Code)
	}
	output := logs.String()
	if strings.Contains(output, "secret-code") || strings.Contains(output, "secret-state") ||
		strings.Contains(output, "provider failure") {
		t.Fatalf("recovery log leaked panic or OAuth query: %s", output)
	}
	if !strings.Contains(output, "/api/auth/wechat/callback") {
		t.Fatalf("recovery log omitted sanitized path: %s", output)
	}
}

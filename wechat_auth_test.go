package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	testWeChatAppID     = "wx-test-app-id"
	testWeChatAppSecret = "wechat-app-secret-must-never-leak"
	testWeChatRedirect  = "https://chat.example.test/api/auth/wechat/callback"
)

func TestLoadWeChatAuthConfig(t *testing.T) {
	t.Setenv("WECHAT_APP_ID", "  "+testWeChatAppID+"  ")
	t.Setenv("WECHAT_APP_SECRET", "  "+testWeChatAppSecret+"  ")
	t.Setenv("WECHAT_REDIRECT_URL", "  "+testWeChatRedirect+"  ")

	config := LoadWeChatAuthConfig()
	if config.AppID != testWeChatAppID || config.AppSecret != testWeChatAppSecret ||
		config.RedirectURL != testWeChatRedirect {
		t.Fatalf("unexpected WeChat configuration: appid=%q redirect=%q", config.AppID, config.RedirectURL)
	}
	if auth := NewWeChatAuth(config, nil); !auth.IsEnabled() || !auth.configured() {
		t.Fatal("complete HTTPS configuration did not enable WeChat authentication")
	}
}

func TestWeChatStatusHandlerIsStateless(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		auth    *WeChatAuth
		enabled bool
	}{
		{name: "complete configuration", auth: newConfiguredWeChatAuth(nil), enabled: true},
		{name: "missing configuration", auth: NewWeChatAuth(WeChatAuthConfig{}, nil), enabled: false},
		{name: "nil auth", auth: nil, enabled: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/auth/wechat/status", nil)
			ginContext, _ := gin.CreateTestContext(recorder)
			ginContext.Request = request
			test.auth.StatusHandler()(ginContext)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode status payload: %v", err)
			}
			if len(payload) != 2 || payload["ok"] != true || payload["wechat_enabled"] != test.enabled {
				t.Fatalf("unexpected status payload: %#v", payload)
			}
			if len(recorder.Result().Cookies()) != 0 {
				t.Fatal("stateless status handler unexpectedly set a cookie")
			}
			assertNoWeChatCredentialLeak(t, recorder.Body.String(), recorder.Header())
		})
	}
}

func TestWeChatLoginUnavailableWithoutCompleteHTTPSConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, config := range []WeChatAuthConfig{
		{},
		{AppID: testWeChatAppID, AppSecret: testWeChatAppSecret},
		{AppID: testWeChatAppID, RedirectURL: testWeChatRedirect},
		{AppID: testWeChatAppID, AppSecret: testWeChatAppSecret, RedirectURL: "/relative"},
		{AppID: testWeChatAppID, AppSecret: testWeChatAppSecret, RedirectURL: "http://chat.example.test/callback"},
		{AppID: testWeChatAppID, AppSecret: testWeChatAppSecret, RedirectURL: "ftp://chat.example.test/callback"},
	} {
		auth := NewWeChatAuth(config, nil)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/auth/wechat/login", nil)
		context, _ := gin.CreateTestContext(recorder)
		context.Request = request
		auth.LoginHandler()(context)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d, want %d for config %#v", recorder.Code, http.StatusServiceUnavailable, config)
		}
		assertNoWeChatCredentialLeak(t, recorder.Body.String(), recorder.Header())
	}
}

func TestWeChatLoginBuildsQRConnectURLAndSecureStateCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := newConfiguredWeChatAuth(nil)
	seenStates := make(map[string]bool)
	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "https://chat.example.test/api/auth/wechat/login", nil)
		context, _ := gin.CreateTestContext(recorder)
		context.Request = request
		auth.LoginHandler()(context)

		if recorder.Code != http.StatusFound {
			t.Fatalf("status=%d, body=%s", recorder.Code, recorder.Body.String())
		}
		location, err := url.Parse(recorder.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse authorization URL: %v", err)
		}
		query := location.Query()
		state := query.Get("state")
		if location.Scheme != "https" || location.Host != "open.weixin.qq.com" ||
			location.Path != "/connect/qrconnect" || location.Fragment != "wechat_redirect" {
			t.Fatalf("unexpected authorization URL: %s", location.String())
		}
		if query.Get("appid") != testWeChatAppID || query.Get("redirect_uri") != testWeChatRedirect ||
			query.Get("response_type") != "code" || query.Get("scope") != "snsapi_login" || !validWeChatState(state) {
			t.Fatalf("unexpected authorization query: %v", query)
		}
		if seenStates[state] {
			t.Fatal("two login attempts reused the same OAuth state")
		}
		seenStates[state] = true

		cookie := responseCookie(t, recorder.Result(), weChatStateCookieName)
		if cookie.Value != state || cookie.Path != "/api/auth/wechat/callback" ||
			!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode ||
			cookie.MaxAge != int(weChatStateLifetime.Seconds()) {
			t.Fatalf("unexpected state cookie: %#v", cookie)
		}
		assertNoWeChatCredentialLeak(t, recorder.Body.String()+location.String(), recorder.Header())
	}
}

func TestWeChatCallbackRejectsStateAndAlwaysClearsCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := newConfiguredWeChatAuth(nil)
	app := newWeChatCallbackTestApp()

	state, err := auth.newState()
	if err != nil {
		t.Fatalf("generate state: %v", err)
	}
	otherState, err := auth.newState()
	if err != nil {
		t.Fatalf("generate other state: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/auth/wechat/callback?code=test-code&state="+url.QueryEscape(otherState), nil)
	request.AddCookie(&http.Cookie{Name: weChatStateCookieName, Value: state})
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request.WithContext(contextWithUserSession(request.Context(), UserSession{ID: strings.Repeat("a", 32)}))
	auth.CallbackHandler(app)(context)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, body=%s", recorder.Code, recorder.Body.String())
	}
	cleared := responseCookie(t, recorder.Result(), weChatStateCookieName)
	if cleared.Value != "" || cleared.MaxAge >= 0 || cleared.Path != "/api/auth/wechat/callback" {
		t.Fatalf("state cookie was not cleared: %#v", cleared)
	}
	assertNoWeChatCredentialLeak(t, recorder.Body.String(), recorder.Header())
}

func TestWeChatCallbackRedirectsAndRotatesOpaqueSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/sns/oauth2/access_token" {
			_, _ = writer.Write([]byte(`{"access_token":"provider-access-token","openid":"openid-callback","unionid":"union-callback"}`))
			return
		}
		if request.URL.Path == "/sns/userinfo" {
			_, _ = writer.Write([]byte(`{"openid":"openid-callback","unionid":"union-callback","nickname":"回调用户","headimgurl":"https://avatar.example/callback.png"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer provider.Close()

	auth := newConfiguredWeChatAuth(provider.Client())
	auth.apiBaseURL = provider.URL
	targetUserID := strings.Repeat("b", 32)
	var boundCurrent, rotatedTarget string
	auth.bindIdentity = func(_ context.Context, _ *sql.DB, current string, profile WeChatProfile) (string, error) {
		boundCurrent = current
		if profile.OpenID != "openid-callback" || profile.UnionID != "union-callback" {
			t.Fatalf("unexpected callback profile: %#v", profile)
		}
		return targetUserID, nil
	}
	auth.createUser = func(context.Context, *App) (string, error) {
		t.Fatal("existing identity must not create a user")
		return "", errors.New("unexpected user creation")
	}

	auth.rotateSession = func(_ *App, _ *gin.Context, target string) error {
		rotatedTarget = target
		return nil
	}
	app := newWeChatCallbackTestApp()
	recorder := invokeWeChatCallback(t, auth, app, url.Values{"code": {"authorization-code"}})
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/?wechat_login=success" {
		t.Fatalf("success callback status=%d location=%q body=%s", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
	}
	if boundCurrent != "" || rotatedTarget != targetUserID {
		t.Fatalf("callback bound current=%q and rotated target=%q", boundCurrent, rotatedTarget)
	}
	assertClearedWeChatStateCookie(t, recorder.Result())
	assertNoWeChatCredentialLeak(t, recorder.Body.String(), recorder.Header())
}

func TestWeChatCallbackWithoutCookieCreatesUserOnlyForNewIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/sns/oauth2/access_token" {
			_, _ = writer.Write([]byte(`{"access_token":"provider-access-token","openid":"openid-new","unionid":"union-new"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"openid":"openid-new","unionid":"union-new","nickname":"新用户"}`))
	}))
	defer provider.Close()

	auth := newConfiguredWeChatAuth(provider.Client())
	auth.apiBaseURL = provider.URL
	createdUserID := strings.Repeat("c", 32)
	bindCalls := 0
	auth.bindIdentity = func(_ context.Context, _ *sql.DB, current string, profile WeChatProfile) (string, error) {
		bindCalls++
		if profile.OpenID != "openid-new" {
			t.Fatalf("unexpected profile: %#v", profile)
		}
		if bindCalls == 1 {
			if current != "" {
				t.Fatalf("first bind current user=%q, want empty", current)
			}
			return "", errWeChatIdentityNeedsUser
		}
		if bindCalls == 2 {
			if current != createdUserID {
				t.Fatalf("second bind current user=%q, want created user", current)
			}
			return createdUserID, nil
		}
		t.Fatalf("unexpected bind call %d", bindCalls)
		return "", errors.New("unexpected bind")
	}
	createCalls := 0
	auth.createUser = func(_ context.Context, _ *App) (string, error) {
		createCalls++
		return createdUserID, nil
	}
	rotatedTarget := ""
	auth.rotateSession = func(_ *App, _ *gin.Context, target string) error {
		rotatedTarget = target
		return nil
	}

	recorder := invokeWeChatCallback(t, auth, newWeChatCallbackTestApp(), url.Values{"code": {"authorization-code"}})
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/?wechat_login=success" {
		t.Fatalf("callback status=%d location=%q body=%s", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
	}
	if bindCalls != 2 || createCalls != 1 || rotatedTarget != createdUserID {
		t.Fatalf("bind calls=%d create calls=%d rotated target=%q", bindCalls, createCalls, rotatedTarget)
	}
}

func TestWeChatCallbackRedirectsRecoverableErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newWeChatCallbackTestApp()

	t.Run("authorization denied", func(t *testing.T) {
		auth := newConfiguredWeChatAuth(nil)
		recorder := invokeWeChatCallback(t, auth, app, url.Values{"error": {"access_denied"}})
		assertWeChatErrorRedirect(t, recorder)
	})

	t.Run("provider error", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"errcode":40029,"errmsg":"` + testWeChatAppSecret + ` provider-access-token"}`))
		}))
		defer provider.Close()
		auth := newConfiguredWeChatAuth(provider.Client())
		auth.apiBaseURL = provider.URL
		recorder := invokeWeChatCallback(t, auth, app, url.Values{"code": {"bad-code"}})
		assertWeChatErrorRedirect(t, recorder)
	})

	t.Run("binding conflict", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/sns/oauth2/access_token" {
				_, _ = writer.Write([]byte(`{"access_token":"provider-access-token","openid":"openid-conflict"}`))
				return
			}
			_, _ = writer.Write([]byte(`{"openid":"openid-conflict","nickname":"冲突用户"}`))
		}))
		defer provider.Close()
		auth := newConfiguredWeChatAuth(provider.Client())
		auth.apiBaseURL = provider.URL
		auth.bindIdentity = func(context.Context, *sql.DB, string, WeChatProfile) (string, error) {
			return "", errWeChatIdentityConflict
		}
		auth.rotateSession = func(*App, *gin.Context, string) error {
			t.Fatal("session must not rotate after an identity conflict")
			return nil
		}
		recorder := invokeWeChatCallback(t, auth, app, url.Values{"code": {"authorization-code"}})
		assertWeChatErrorRedirect(t, recorder)
	})
}

func TestWeChatExchangeFetchesProfileWithoutCredentialLeak(t *testing.T) {
	var mu sync.Mutex
	requests := make([]string, 0, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.URL.Path)
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/sns/oauth2/access_token":
			if request.URL.Query().Get("appid") != testWeChatAppID ||
				request.URL.Query().Get("secret") != testWeChatAppSecret ||
				request.URL.Query().Get("code") != "authorization-code" ||
				request.URL.Query().Get("grant_type") != "authorization_code" {
				t.Errorf("unexpected token request query")
			}
			_, _ = writer.Write([]byte(`{"access_token":"provider-access-token","openid":"openid-123","unionid":"union-from-token"}`))
		case "/sns/userinfo":
			if request.URL.Query().Get("access_token") != "provider-access-token" ||
				request.URL.Query().Get("openid") != "openid-123" || request.URL.Query().Get("lang") != "zh_CN" {
				t.Errorf("unexpected userinfo request query")
			}
			_, _ = writer.Write([]byte(`{"openid":"openid-123","unionid":"union-123","nickname":"微信用户","headimgurl":"http://avatar.example.test/user.png"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer provider.Close()

	auth := newConfiguredWeChatAuth(provider.Client())
	auth.apiBaseURL = provider.URL
	profile, err := auth.exchangeProfile(context.Background(), "authorization-code")
	if err != nil {
		t.Fatalf("exchange profile: %v", err)
	}
	if profile.OpenID != "openid-123" || profile.UnionID != "union-123" ||
		profile.Nickname != "微信用户" || profile.AvatarURL != "https://avatar.example.test/user.png" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0] != "/sns/oauth2/access_token" || requests[1] != "/sns/userinfo" {
		t.Fatalf("unexpected provider request sequence: %v", requests)
	}
	assertNoWeChatCredentialLeak(t, fmt.Sprint(profile), http.Header{})
}

func TestNormalizeWeChatAvatarURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "absolute https", input: " https://avatar.example/user.png?size=96 ", want: "https://avatar.example/user.png?size=96"},
		{name: "upgrade http", input: "http://avatar.example/user.png", want: "https://avatar.example/user.png"},
		{name: "reject userinfo", input: "https://user:password@avatar.example/user.png", want: ""},
		{name: "reject relative", input: "/avatar/user.png", want: ""},
		{name: "reject protocol relative", input: "//avatar.example/user.png", want: ""},
		{name: "reject data", input: "data:image/png;base64,AAAA", want: ""},
		{name: "reject javascript", input: "javascript:alert(1)", want: ""},
		{name: "reject malformed", input: "https://[invalid", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeWeChatAvatarURL(test.input); got != test.want {
				t.Fatalf("normalize avatar URL=%q, want %q", got, test.want)
			}
		})
	}
}

func TestWeChatExchangeDropsUnsafeAvatarWithoutFailingLogin(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/sns/oauth2/access_token" {
			_, _ = writer.Write([]byte(`{"access_token":"provider-access-token","openid":"openid-unsafe-avatar"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"openid":"openid-unsafe-avatar","nickname":"安全登录","headimgurl":"data:text/html,unsafe"}`))
	}))
	defer provider.Close()

	auth := newConfiguredWeChatAuth(provider.Client())
	auth.apiBaseURL = provider.URL
	profile, err := auth.exchangeProfile(context.Background(), "authorization-code")
	if err != nil {
		t.Fatalf("unsafe avatar must not reject login: %v", err)
	}
	if profile.OpenID != "openid-unsafe-avatar" || profile.AvatarURL != "" {
		t.Fatalf("unexpected sanitized profile: %#v", profile)
	}
}

func TestWeChatProviderResponseLimitTimeoutAndErrorsAreSanitized(t *testing.T) {
	t.Run("response limit", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(strings.Repeat("x", 129)))
		}))
		defer provider.Close()
		auth := newConfiguredWeChatAuth(provider.Client())
		auth.apiBaseURL = provider.URL
		auth.maxResponseBytes = 128
		_, err := auth.exchangeProfile(context.Background(), "code")
		if !errors.Is(err, errWeChatResponseTooLong) {
			t.Fatalf("error=%v, want response-too-long", err)
		}
		assertNoWeChatCredentialLeak(t, err.Error(), http.Header{})
	})

	t.Run("timeout", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}))
		defer provider.Close()
		auth := newConfiguredWeChatAuth(provider.Client())
		auth.apiBaseURL = provider.URL
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, err := auth.exchangeProfile(ctx, "code")
		if !errors.Is(err, errWeChatProvider) {
			t.Fatalf("error=%v, want provider error", err)
		}
		if time.Since(started) > time.Second {
			t.Fatal("provider request did not honor context timeout")
		}
		assertNoWeChatCredentialLeak(t, err.Error(), http.Header{})
	})

	t.Run("logical error", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"errcode":40029,"errmsg":"` + testWeChatAppSecret + ` provider-access-token"}`))
		}))
		defer provider.Close()
		auth := newConfiguredWeChatAuth(provider.Client())
		auth.apiBaseURL = provider.URL
		_, err := auth.exchangeProfile(context.Background(), "code")
		if !errors.Is(err, errWeChatProvider) {
			t.Fatalf("error=%v, want provider error", err)
		}
		assertNoWeChatCredentialLeak(t, err.Error(), http.Header{})
	})
}

func TestWeChatIdentitySchemaDoesNotPersistOAuthTokens(t *testing.T) {
	schema := strings.ToUpper(weChatIdentitySchema + "\n" + weChatIdentityUnionIndex)
	for _, required := range []string{
		"OPENID TEXT PRIMARY KEY",
		"USER_ID UUID NOT NULL UNIQUE",
		"REFERENCES USERS(USER_ID) ON DELETE CASCADE",
		"CREATE UNIQUE INDEX",
		"ON WECHAT_IDENTITIES(UNIONID)",
		"WHERE UNIONID <> ''",
		"TIMESTAMPTZ",
	} {
		if !strings.Contains(schema, required) {
			t.Errorf("schema missing %q", required)
		}
	}
	for _, forbidden := range []string{"ACCESS_TOKEN", "REFRESH_TOKEN", "APPSECRET", "APP_SECRET"} {
		if strings.Contains(schema, forbidden) {
			t.Errorf("schema persists forbidden credential field %q", forbidden)
		}
	}
}

func TestWeChatIdentityBindingIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := OpenPostgres(ctx, databaseURL, 8, 4)
	if err != nil {
		t.Fatalf("open test PostgreSQL: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := initWeChatAuthSchema(ctx, db); err != nil {
		t.Fatalf("initialize WeChat schema: %v", err)
	}

	userIDs := []string{newWeChatTestUserID(t), newWeChatTestUserID(t), newWeChatTestUserID(t), newWeChatTestUserID(t)}
	openID := "openid-" + newWeChatTestUserID(t)
	sameUnionOpenID := "openid-same-union-" + newWeChatTestUserID(t)
	concurrentOpenID := "openid-concurrent-" + newWeChatTestUserID(t)
	conflictingOpenID := "openid-conflict-" + newWeChatTestUserID(t)
	defer cleanupWeChatIntegrationRows(t, db, userIDs, []string{openID, sameUnionOpenID, concurrentOpenID, conflictingOpenID})

	var existingUsers int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&existingUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}
	store := PostgresUserStore{DB: db, MaxUsers: existingUsers + len(userIDs)}
	for _, userID := range userIDs {
		created, err := store.EnsureUser(ctx, userID)
		if err != nil || !created {
			t.Fatalf("ensure integration user: created=%v err=%v", created, err)
		}
	}

	firstProfile := WeChatProfile{OpenID: openID, UnionID: "union-a", Nickname: "first", AvatarURL: "https://avatar.example/first.png"}
	if _, err := bindWeChatIdentity(ctx, db, "", firstProfile); !errors.Is(err, errWeChatIdentityNeedsUser) {
		t.Fatalf("new identity without local user error=%v, want needs-user", err)
	}

	bound, err := bindWeChatIdentity(ctx, db, userIDs[0], firstProfile)
	if err != nil || bound != userIDs[0] {
		t.Fatalf("first bind returned user=%q err=%v", bound, err)
	}
	updatedProfile := WeChatProfile{OpenID: sameUnionOpenID, UnionID: "union-a", Nickname: "updated", AvatarURL: "https://avatar.example/updated.png"}
	bound, err = bindWeChatIdentity(ctx, db, "", updatedProfile)
	if err != nil || bound != userIDs[0] {
		t.Fatalf("existing UnionID returned user=%q err=%v, want %q", bound, err, userIDs[0])
	}
	profile, authenticated, err := lookupWeChatProfile(ctx, db, userIDs[0])
	if err != nil || !authenticated || profile.Nickname != "updated" {
		t.Fatalf("lookup profile=%#v authenticated=%v err=%v", profile, authenticated, err)
	}
	if _, _, err := lookupWeChatProfile(ctx, db, userIDs[1]); err != nil {
		t.Fatalf("lookup unbound user: %v", err)
	}
	if _, err := bindWeChatIdentity(ctx, db, userIDs[0], WeChatProfile{OpenID: conflictingOpenID}); !errors.Is(err, errWeChatIdentityConflict) {
		t.Fatalf("second OpenID for one user error=%v, want identity conflict", err)
	}

	start := make(chan struct{})
	results := make(chan struct {
		userID string
		err    error
	}, 2)
	for _, userID := range userIDs[2:] {
		go func(candidate string) {
			<-start
			boundUser, bindErr := bindWeChatIdentity(ctx, db, candidate, WeChatProfile{
				OpenID:   concurrentOpenID + "-" + candidate[:8],
				UnionID:  "union-" + concurrentOpenID,
				Nickname: "concurrent",
			})
			results <- struct {
				userID string
				err    error
			}{boundUser, bindErr}
		}(userID)
	}
	close(start)
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil || first.userID == "" || first.userID != second.userID {
		t.Fatalf("concurrent bind results: first=%+v second=%+v", first, second)
	}
}

func newConfiguredWeChatAuth(client *http.Client) *WeChatAuth {
	return NewWeChatAuth(WeChatAuthConfig{
		AppID:       testWeChatAppID,
		AppSecret:   testWeChatAppSecret,
		RedirectURL: testWeChatRedirect,
	}, client)
}

func newWeChatCallbackTestApp() *App {
	db := &sql.DB{}
	return &App{
		db:       db,
		sessions: PostgresSessionStore{DB: db},
	}
}

func invokeWeChatCallback(
	t *testing.T,
	auth *WeChatAuth,
	app *App,
	query url.Values,
) *httptest.ResponseRecorder {
	t.Helper()
	state, err := auth.newState()
	if err != nil {
		t.Fatalf("generate callback state: %v", err)
	}
	query = cloneURLValues(query)
	query.Set("state", state)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/wechat/callback?"+query.Encode(), nil)
	request.AddCookie(&http.Cookie{Name: weChatStateCookieName, Value: state})
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = request
	auth.CallbackHandler(app)(ginContext)
	return recorder
}

func cloneURLValues(source url.Values) url.Values {
	cloned := make(url.Values, len(source))
	for key, values := range source {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func assertWeChatErrorRedirect(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/?wechat_login=error" {
		t.Fatalf("error callback status=%d location=%q body=%s", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
	}
	assertClearedWeChatStateCookie(t, recorder.Result())
	assertNoWeChatCredentialLeak(t, recorder.Body.String(), recorder.Header())
}

func assertClearedWeChatStateCookie(t *testing.T, response *http.Response) {
	t.Helper()
	cookie := responseCookie(t, response, weChatStateCookieName)
	if cookie.Value != "" || cookie.MaxAge >= 0 || cookie.Path != "/api/auth/wechat/callback" {
		t.Fatalf("state cookie was not cleared: %#v", cookie)
	}
}

func responseCookie(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response does not contain cookie %q", name)
	return nil
}

func assertNoWeChatCredentialLeak(t *testing.T, body string, headers http.Header) {
	t.Helper()
	content := body + "\n" + fmt.Sprint(headers)
	for _, secret := range []string{testWeChatAppSecret, "provider-access-token"} {
		if strings.Contains(content, secret) {
			t.Fatalf("response or error leaked a WeChat credential")
		}
	}
}

func newWeChatTestUserID(t *testing.T) string {
	t.Helper()
	userID, err := newUserID()
	if err != nil {
		t.Fatalf("generate user id: %v", err)
	}
	return userID
}

func cleanupWeChatIntegrationRows(t *testing.T, db *sql.DB, userIDs, openIDs []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, openID := range openIDs {
		_, _ = db.ExecContext(ctx, `DELETE FROM wechat_identities WHERE openid = $1`, openID)
	}
	for _, userID := range userIDs {
		normalized, err := normalizePostgresUserUUID(userID)
		if err == nil {
			_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1::uuid`, normalized)
		}
	}
}

func TestWeChatSessionPayloadNeverContainsInternalIdentifiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := newConfiguredWeChatAuth(nil)
	localUserID := strings.Repeat("a", 32)
	auth.lookupProfile = func(_ context.Context, _ *sql.DB, userID string) (WeChatProfile, bool, error) {
		if userID != localUserID {
			t.Fatalf("lookup user=%q, want current session", userID)
		}
		return WeChatProfile{
			OpenID:    "openid-must-not-be-public",
			UnionID:   "unionid-must-not-be-public",
			Nickname:  "微信用户",
			AvatarURL: "https://avatar.example/user.png",
		}, true, nil
	}
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request = request.WithContext(contextWithUserSession(request.Context(), UserSession{ID: localUserID}))
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = request
	auth.SessionHandler(&App{db: &sql.DB{}})(ginContext)
	if recorder.Code != http.StatusOK {
		t.Fatalf("session status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session payload: %v", err)
	}
	if payload["ok"] != true || payload["authenticated"] != true || payload["wechat_enabled"] != true {
		t.Fatalf("unexpected session payload: %#v", payload)
	}
	user, ok := payload["user"].(map[string]any)
	if !ok || len(user) != 2 || user["nickname"] != "微信用户" || user["avatar_url"] != "https://avatar.example/user.png" {
		t.Fatalf("unexpected public user payload: %#v", payload["user"])
	}
	content := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"openid", "unionid", "user_id", localUserID} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("session payload contains internal identifier %q", forbidden)
		}
	}
}

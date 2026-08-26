package main

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

const (
	weChatAuthorizationURL = "https://open.weixin.qq.com/connect/qrconnect"
	weChatAPIBaseURL       = "https://api.weixin.qq.com"
	weChatStateCookieName  = "darling_wechat_oauth_state"
	weChatStateBytes       = 32
	weChatStateLifetime    = 10 * time.Minute
	weChatRequestTimeout   = 10 * time.Second
	weChatMaxResponseBytes = int64(256 << 10)
	weChatSchemaLockID     = int64(0x4461726c696e6757)
	weChatIdentityLockID   = int64(0x4461726c696e6749)
)

var (
	errWeChatAuthUnavailable   = errors.New("WeChat authentication is unavailable")
	errWeChatInvalidState      = errors.New("invalid WeChat OAuth state")
	errWeChatProvider          = errors.New("WeChat OAuth provider request failed")
	errWeChatResponseTooLong   = errors.New("WeChat OAuth provider response is too large")
	errWeChatIdentityNeedsUser = errors.New("WeChat identity needs a local user")
	errWeChatIdentityConflict  = errors.New(
		"local user is already linked to a different WeChat identity",
	)
)

// WeChatAuthConfig contains the credentials of a WeChat Open Platform website
// application. AppSecret must never be sent to the browser or written to logs.
type WeChatAuthConfig struct {
	AppID       string
	AppSecret   string
	RedirectURL string
}

// LoadWeChatAuthConfig reads the optional WeChat website OAuth configuration.
// Authentication is enabled only when all three required values are present.
func LoadWeChatAuthConfig() WeChatAuthConfig {
	return WeChatAuthConfig{
		AppID:       strings.TrimSpace(os.Getenv("WECHAT_APP_ID")),
		AppSecret:   strings.TrimSpace(os.Getenv("WECHAT_APP_SECRET")),
		RedirectURL: strings.TrimSpace(os.Getenv("WECHAT_REDIRECT_URL")),
	}
}

type WeChatProfile struct {
	OpenID    string
	UnionID   string
	Nickname  string
	AvatarURL string
}

type weChatTokenResponse struct {
	AccessToken string `json:"access_token"`
	OpenID      string `json:"openid"`
	UnionID     string `json:"unionid"`
	ErrCode     int    `json:"errcode"`
}

type weChatUserInfoResponse struct {
	OpenID     string `json:"openid"`
	UnionID    string `json:"unionid"`
	Nickname   string `json:"nickname"`
	HeadImgURL string `json:"headimgurl"`
	ErrCode    int    `json:"errcode"`
}

type weChatIdentityBinder func(context.Context, *sql.DB, string, WeChatProfile) (string, error)
type weChatProfileLookup func(context.Context, *sql.DB, string) (WeChatProfile, bool, error)
type weChatSessionRotator func(*App, *gin.Context, string) error
type weChatUserCreator func(context.Context, *App) (string, error)

type WeChatAuth struct {
	config           WeChatAuthConfig
	client           *http.Client
	authorizationURL string
	apiBaseURL       string
	requestTimeout   time.Duration
	maxResponseBytes int64
	randReader       io.Reader
	bindIdentity     weChatIdentityBinder
	lookupProfile    weChatProfileLookup
	rotateSession    weChatSessionRotator
	createUser       weChatUserCreator
}

func NewWeChatAuth(config WeChatAuthConfig, client *http.Client) *WeChatAuth {
	config.AppID = strings.TrimSpace(config.AppID)
	config.AppSecret = strings.TrimSpace(config.AppSecret)
	config.RedirectURL = strings.TrimSpace(config.RedirectURL)

	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	// OAuth credentials are query parameters in the WeChat API. Refusing HTTP
	// redirects prevents a provider/proxy redirect from forwarding them elsewhere.
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &WeChatAuth{
		config:           config,
		client:           &clientCopy,
		authorizationURL: weChatAuthorizationURL,
		apiBaseURL:       weChatAPIBaseURL,
		requestTimeout:   weChatRequestTimeout,
		maxResponseBytes: weChatMaxResponseBytes,
		randReader:       cryptorand.Reader,
		bindIdentity:     bindWeChatIdentity,
		lookupProfile:    lookupWeChatProfile,
		rotateSession: func(app *App, c *gin.Context, userID string) error {
			return app.rotateUserSession(c, userID)
		},
		createUser: func(ctx context.Context, app *App) (string, error) {
			return app.createAnonymousUser(ctx)
		},
	}
}

func (w *WeChatAuth) IsEnabled() bool {
	return w != nil && w.config.AppID != "" && w.config.AppSecret != "" && w.config.RedirectURL != ""
}

func (w *WeChatAuth) configured() bool {
	if !w.IsEnabled() || w.client == nil || w.randReader == nil ||
		w.bindIdentity == nil || w.lookupProfile == nil || w.rotateSession == nil || w.createUser == nil {
		return false
	}
	if w.config.AppID == "" || w.config.AppSecret == "" || w.config.RedirectURL == "" {
		return false
	}
	redirect, err := url.Parse(w.config.RedirectURL)
	return err == nil && redirect.IsAbs() && redirect.Host != "" &&
		redirect.Scheme == "https" && redirect.User == nil
}

// StatusHandler exposes only whether WeChat login is ready. It is intentionally
// stateless so public capability checks never create a local user or DB session.
func (w *WeChatAuth) StatusHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{
			"ok":             true,
			"wechat_enabled": w != nil && w.configured(),
		})
	}
}

func (w *WeChatAuth) LoginHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !w.configured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "WeChat login is unavailable"})
			return
		}

		state, err := w.newState()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "start WeChat login failed"})
			return
		}
		authorizationURL, err := w.buildAuthorizationURL(state)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "WeChat login is unavailable"})
			return
		}
		w.writeStateCookie(c, state, int(weChatStateLifetime.Seconds()), time.Now().Add(weChatStateLifetime))
		c.Redirect(http.StatusFound, authorizationURL)
	}
}

func (w *WeChatAuth) CallbackHandler(app *App) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !w.configured() || app == nil || app.db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "WeChat login is unavailable"})
			return
		}

		stateCookie, cookieErr := c.Cookie(weChatStateCookieName)
		w.clearStateCookie(c)
		state := c.Query("state")
		if cookieErr != nil || !validWeChatState(state) || !constantTimeStringEqual(stateCookie, state) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired WeChat login state"})
			return
		}
		if c.Query("error") != "" {
			w.redirectResult(c, false)
			return
		}
		code := strings.TrimSpace(c.Query("code"))
		if code == "" || len(code) > 1024 || strings.IndexFunc(code, unicode.IsControl) >= 0 {
			w.redirectResult(c, false)
			return
		}
		currentUserID, _, err := app.sessions.Resolve(c.Request.Context(), readUserCookie(c))
		if err != nil {
			w.redirectResult(c, false)
			return
		}

		timeout := w.requestTimeout
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = weChatRequestTimeout
		}
		providerCtx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		profile, err := w.exchangeProfile(providerCtx, code)
		if err != nil {
			// Deliberately do not expose or log err: transport errors can include a
			// request URL containing AppSecret or the OAuth access token.
			w.redirectResult(c, false)
			return
		}
		boundUserID, err := w.bindIdentity(providerCtx, app.db, currentUserID, profile)
		if errors.Is(err, errWeChatIdentityNeedsUser) {
			currentUserID, err = w.createUser(providerCtx, app)
			if err == nil {
				boundUserID, err = w.bindIdentity(providerCtx, app.db, currentUserID, profile)
			}
		}
		if err != nil {
			if errors.Is(err, errWeChatIdentityConflict) {
				w.redirectResult(c, false)
				return
			}
			w.redirectResult(c, false)
			return
		}

		if err := w.rotateSession(app, c, boundUserID); err != nil {
			w.redirectResult(c, false)
			return
		}
		w.redirectResult(c, true)
	}
}

func (w *WeChatAuth) redirectResult(c *gin.Context, success bool) {
	result := "error"
	if success {
		result = "success"
	}
	c.Redirect(http.StatusSeeOther, "/?wechat_login="+result)
}

// SessionHandler returns the public authentication state. It intentionally
// omits the local user id, OpenID and UnionID.
func (w *WeChatAuth) SessionHandler(app *App) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		payload := gin.H{
			"ok":             true,
			"authenticated":  false,
			"wechat_enabled": w != nil && w.configured(),
		}
		if w == nil || app == nil || app.db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session service is unavailable"})
			return
		}
		session, err := userSessionFromContext(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user session is unavailable"})
			return
		}
		profile, authenticated, err := w.lookupProfile(c.Request.Context(), app.db, session.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "read session failed"})
			return
		}
		payload["authenticated"] = authenticated
		if authenticated {
			payload["user"] = gin.H{
				"nickname":   profile.Nickname,
				"avatar_url": profile.AvatarURL,
			}
		}
		c.JSON(http.StatusOK, payload)
	}
}

func (w *WeChatAuth) newState() (string, error) {
	value := make([]byte, weChatStateBytes)
	if _, err := io.ReadFull(w.randReader, value); err != nil {
		return "", errors.New("generate WeChat OAuth state")
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validWeChatState(state string) bool {
	if len(state) != base64.RawURLEncoding.EncodedLen(weChatStateBytes) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(state)
	return err == nil && len(decoded) == weChatStateBytes
}

func constantTimeStringEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (w *WeChatAuth) buildAuthorizationURL(state string) (string, error) {
	if !validWeChatState(state) {
		return "", errWeChatInvalidState
	}
	authorization, err := url.Parse(w.authorizationURL)
	if err != nil || !authorization.IsAbs() {
		return "", errWeChatAuthUnavailable
	}
	query := authorization.Query()
	query.Set("appid", w.config.AppID)
	query.Set("redirect_uri", w.config.RedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", "snsapi_login")
	query.Set("state", state)
	authorization.RawQuery = query.Encode()
	authorization.Fragment = "wechat_redirect"
	return authorization.String(), nil
}

func (w *WeChatAuth) writeStateCookie(c *gin.Context, value string, maxAge int, expires time.Time) {
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     weChatStateCookieName,
		Value:    value,
		Path:     w.stateCookiePath(),
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (w *WeChatAuth) clearStateCookie(c *gin.Context) {
	w.writeStateCookie(c, "", -1, time.Unix(1, 0))
}

func (w *WeChatAuth) stateCookiePath() string {
	redirect, err := url.Parse(w.config.RedirectURL)
	if err != nil || redirect.Path == "" || redirect.Path[0] != '/' {
		return "/"
	}
	return redirect.Path
}

func (w *WeChatAuth) exchangeProfile(ctx context.Context, code string) (WeChatProfile, error) {
	tokenURL, err := url.Parse(strings.TrimRight(w.apiBaseURL, "/") + "/sns/oauth2/access_token")
	if err != nil || !tokenURL.IsAbs() {
		return WeChatProfile{}, errWeChatAuthUnavailable
	}
	query := tokenURL.Query()
	query.Set("appid", w.config.AppID)
	query.Set("secret", w.config.AppSecret)
	query.Set("code", code)
	query.Set("grant_type", "authorization_code")
	tokenURL.RawQuery = query.Encode()

	var token weChatTokenResponse
	if err := w.getJSON(ctx, tokenURL, &token); err != nil {
		return WeChatProfile{}, err
	}
	if token.ErrCode != 0 || !validWeChatOpenID(token.OpenID) || token.AccessToken == "" || len(token.AccessToken) > 4096 {
		return WeChatProfile{}, errWeChatProvider
	}

	userInfoURL, err := url.Parse(strings.TrimRight(w.apiBaseURL, "/") + "/sns/userinfo")
	if err != nil || !userInfoURL.IsAbs() {
		return WeChatProfile{}, errWeChatAuthUnavailable
	}
	query = userInfoURL.Query()
	query.Set("access_token", token.AccessToken)
	query.Set("openid", token.OpenID)
	query.Set("lang", "zh_CN")
	userInfoURL.RawQuery = query.Encode()

	var userInfo weChatUserInfoResponse
	if err := w.getJSON(ctx, userInfoURL, &userInfo); err != nil {
		return WeChatProfile{}, err
	}
	if userInfo.ErrCode != 0 || userInfo.OpenID != token.OpenID || !validWeChatOpenID(userInfo.OpenID) {
		return WeChatProfile{}, errWeChatProvider
	}
	unionID := strings.TrimSpace(userInfo.UnionID)
	if unionID == "" {
		unionID = strings.TrimSpace(token.UnionID)
	}
	profile := WeChatProfile{
		OpenID:    userInfo.OpenID,
		UnionID:   unionID,
		Nickname:  strings.TrimSpace(userInfo.Nickname),
		AvatarURL: strings.TrimSpace(userInfo.HeadImgURL),
	}
	profile.AvatarURL = normalizeWeChatAvatarURL(profile.AvatarURL)
	if !validWeChatProfile(profile) {
		return WeChatProfile{}, errWeChatProvider
	}
	return profile, nil
}

func (w *WeChatAuth) getJSON(ctx context.Context, endpoint *url.URL, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errWeChatProvider
	}
	request.Header.Set("Accept", "application/json")
	response, err := w.client.Do(request)
	if err != nil {
		return errWeChatProvider
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errWeChatProvider
	}
	limit := w.maxResponseBytes
	if limit <= 0 || limit > 1<<20 {
		limit = weChatMaxResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return errWeChatProvider
	}
	if int64(len(body)) > limit {
		return errWeChatResponseTooLong
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return errWeChatProvider
	}
	return nil
}

func normalizeWeChatAvatarURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	if strings.EqualFold(parsed.Scheme, "http") {
		parsed.Scheme = "https"
	}
	if parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func validWeChatOpenID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 128 && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validWeChatProfile(profile WeChatProfile) bool {
	return validWeChatOpenID(profile.OpenID) &&
		validOptionalWeChatField(profile.UnionID, 128) &&
		validOptionalWeChatField(profile.Nickname, 512) &&
		validOptionalWeChatField(profile.AvatarURL, 4096)
}

func validOptionalWeChatField(value string, maxBytes int) bool {
	return len(value) <= maxBytes && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

const weChatIdentitySchema = `CREATE TABLE IF NOT EXISTS wechat_identities (
  openid TEXT PRIMARY KEY,
  user_id UUID NOT NULL UNIQUE REFERENCES users(user_id) ON DELETE CASCADE,
  unionid TEXT NOT NULL DEFAULT '',
  nickname TEXT NOT NULL DEFAULT '',
  avatar_url TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

const weChatIdentityUnionIndex = `CREATE UNIQUE INDEX IF NOT EXISTS uq_wechat_identities_unionid_nonempty
  ON wechat_identities(unionid)
  WHERE unionid <> ''`

func initWeChatAuthSchema(ctx context.Context, db *sql.DB) error {
	if ctx == nil || db == nil {
		return errors.New("initialize WeChat auth schema: database is unavailable")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("initialize WeChat auth schema")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, weChatSchemaLockID); err != nil {
		return errors.New("initialize WeChat auth schema")
	}
	if _, err := tx.ExecContext(ctx, weChatIdentitySchema); err != nil {
		return errors.New("initialize WeChat auth schema")
	}
	if _, err := tx.ExecContext(ctx, weChatIdentityUnionIndex); err != nil {
		return errors.New("initialize WeChat auth schema")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("initialize WeChat auth schema")
	}
	return nil
}

// bindWeChatIdentity atomically binds a previously unseen OpenID to the
// current anonymous user. If the OpenID already exists, the existing user is
// returned so the caller can switch the browser session to that account.
func bindWeChatIdentity(
	ctx context.Context,
	db *sql.DB,
	currentUserID string,
	profile WeChatProfile,
) (string, error) {
	if ctx == nil || db == nil {
		return "", errors.New("bind WeChat identity: database is unavailable")
	}
	profile.OpenID = strings.TrimSpace(profile.OpenID)
	profile.UnionID = strings.TrimSpace(profile.UnionID)
	profile.Nickname = strings.TrimSpace(profile.Nickname)
	profile.AvatarURL = strings.TrimSpace(profile.AvatarURL)
	profile.AvatarURL = normalizeWeChatAvatarURL(profile.AvatarURL)
	if !validWeChatProfile(profile) {
		return "", errors.New("bind WeChat identity: invalid profile")
	}
	if err := initWeChatAuthSchema(ctx, db); err != nil {
		return "", err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", errors.New("bind WeChat identity")
	}
	defer func() { _ = tx.Rollback() }()
	// The database constraint is the final race barrier. The advisory lock also
	// makes the read-then-insert path deterministic for concurrent OpenID/UnionID
	// combinations and lets both callers switch to the same winning user.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, weChatIdentityLockID); err != nil {
		return "", errors.New("bind WeChat identity")
	}

	type linkedIdentity struct {
		openID  string
		userID  string
		unionID string
	}
	rows, err := tx.QueryContext(ctx, `
SELECT openid, user_id::text, unionid
FROM wechat_identities
WHERE openid = $1 OR ($2 <> '' AND unionid = $2)
FOR UPDATE`, profile.OpenID, profile.UnionID)
	if err != nil {
		return "", errors.New("bind WeChat identity")
	}
	linked := make([]linkedIdentity, 0, 2)
	for rows.Next() {
		var identity linkedIdentity
		if err := rows.Scan(&identity.openID, &identity.userID, &identity.unionID); err != nil {
			_ = rows.Close()
			return "", errors.New("bind WeChat identity")
		}
		linked = append(linked, identity)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return "", errors.New("bind WeChat identity")
	}
	if len(linked) > 1 {
		return "", errWeChatIdentityConflict
	}

	var boundUUID string
	if len(linked) == 1 {
		identity := linked[0]
		if identity.unionID != "" && profile.UnionID != "" && identity.unionID != profile.UnionID {
			return "", errWeChatIdentityConflict
		}
		unionID := identity.unionID
		if unionID == "" {
			unionID = profile.UnionID
		}
		err = tx.QueryRowContext(ctx, `
UPDATE wechat_identities
SET unionid = $2,
    nickname = $3,
    avatar_url = $4,
    updated_at = CURRENT_TIMESTAMP
WHERE openid = $1
RETURNING user_id::text`, identity.openID, unionID, profile.Nickname, profile.AvatarURL).Scan(&boundUUID)
	} else {
		if currentUserID == "" {
			return "", errWeChatIdentityNeedsUser
		}
		normalizedCurrentUser, normalizeErr := normalizePostgresUserUUID(currentUserID)
		if normalizeErr != nil {
			return "", errors.New("bind WeChat identity: invalid local user")
		}
		err = tx.QueryRowContext(ctx, `
INSERT INTO wechat_identities(openid, user_id, unionid, nickname, avatar_url)
VALUES ($1, $2::uuid, $3, $4, $5)
RETURNING user_id::text`,
			profile.OpenID,
			normalizedCurrentUser,
			profile.UnionID,
			profile.Nickname,
			profile.AvatarURL,
		).Scan(&boundUUID)
	}
	if err != nil {
		var postgresError *pq.Error
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return "", errWeChatIdentityConflict
		}
		return "", errors.New("bind WeChat identity")
	}
	if err := tx.Commit(); err != nil {
		return "", errors.New("bind WeChat identity")
	}
	boundUserID := strings.ReplaceAll(strings.ToLower(boundUUID), "-", "")
	if _, valid := normalizeUserID(boundUserID); !valid {
		return "", errors.New("bind WeChat identity: invalid linked user")
	}
	return boundUserID, nil
}

func lookupWeChatProfile(
	ctx context.Context,
	db *sql.DB,
	userID string,
) (WeChatProfile, bool, error) {
	if ctx == nil || db == nil {
		return WeChatProfile{}, false, errors.New("read WeChat profile: database is unavailable")
	}
	normalizedUserID, err := normalizePostgresUserUUID(userID)
	if err != nil {
		return WeChatProfile{}, false, errors.New("read WeChat profile: invalid local user")
	}
	if err := initWeChatAuthSchema(ctx, db); err != nil {
		return WeChatProfile{}, false, err
	}
	var profile WeChatProfile
	err = db.QueryRowContext(ctx, `
SELECT openid, unionid, nickname, avatar_url
FROM wechat_identities
WHERE user_id = $1::uuid`, normalizedUserID).Scan(
		&profile.OpenID,
		&profile.UnionID,
		&profile.Nickname,
		&profile.AvatarURL,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WeChatProfile{}, false, nil
	}
	if err != nil {
		return WeChatProfile{}, false, errors.New("read WeChat profile")
	}
	profile.AvatarURL = normalizeWeChatAvatarURL(profile.AvatarURL)
	if !validWeChatProfile(profile) {
		return WeChatProfile{}, false, errors.New("read WeChat profile: invalid profile")
	}
	return profile, true, nil
}

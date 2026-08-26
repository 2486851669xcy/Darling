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

func TestBuildMomentActionPromptSeparatesMomentAuthors(t *testing.T) {
	character := &Character{Name: "露娜"}
	moments := []Moment{
		{
			ID:      101,
			Author:  "user",
			Content: "USER_MOMENT_MARKER",
			Comments: []MomentComment{
				{Author: "user", Content: "USER_COMMENT_MARKER"},
				{Author: "character", Content: "SELF_COMMENT_MARKER"},
				{Author: "unexpected", Content: "UNKNOWN_COMMENT_MARKER"},
			},
			Likes: []MomentLike{
				{Author: "user"},
				{Author: "character"},
				{Author: "unexpected"},
			},
		},
		{ID: 202, Author: "character", Content: "SELF_MOMENT_MARKER"},
		{ID: 303, Author: "unexpected", Content: "UNKNOWN_MOMENT_MARKER"},
	}

	prompt := buildMomentActionPrompt(character, moments, true)
	userHeader := "【用户发布的朋友圈（唯一可评论目标和用户信号）】"
	selfHeader := "【角色自己发布的朋友圈（仅用于自我记忆和避免重复）】"
	userStart := strings.Index(prompt, userHeader)
	selfStart := strings.Index(prompt, selfHeader)
	outputStart := strings.Index(prompt, "请输出：")
	if userStart < 0 || selfStart <= userStart || outputStart <= selfStart {
		t.Fatalf("prompt sections are missing or out of order:\n%s", prompt)
	}

	userSection := prompt[userStart:selfStart]
	selfSection := prompt[selfStart:outputStart]
	for _, want := range []string{
		"ID=101 author_role=user",
		"USER_MOMENT_MARKER",
		"user:USER_COMMENT_MARKER",
		"character_self:SELF_COMMENT_MARKER",
		"点赞：user、character_self",
	} {
		if !strings.Contains(userSection, want) {
			t.Errorf("user section does not contain %q:\n%s", want, userSection)
		}
	}
	for _, forbidden := range []string{"SELF_MOMENT_MARKER", "UNKNOWN_MOMENT_MARKER", "UNKNOWN_COMMENT_MARKER"} {
		if strings.Contains(userSection, forbidden) {
			t.Errorf("user section contains %q:\n%s", forbidden, userSection)
		}
	}

	if !strings.Contains(selfSection, "ID=202 author_role=character_self") ||
		!strings.Contains(selfSection, "SELF_MOMENT_MARKER") {
		t.Errorf("self section does not contain the character moment:\n%s", selfSection)
	}
	for _, forbidden := range []string{"USER_MOMENT_MARKER", "UNKNOWN_MOMENT_MARKER"} {
		if strings.Contains(selfSection, forbidden) {
			t.Errorf("self section contains %q:\n%s", forbidden, selfSection)
		}
	}

	for _, rule := range []string{
		"绝不能当成用户的近况、情绪或经历",
		"绝不能仅据此向用户提问",
		"comment 的 moment_id 必须来自【用户发布的朋友圈】",
	} {
		if !strings.Contains(prompt, rule) {
			t.Errorf("prompt is missing identity rule %q", rule)
		}
	}
	if strings.Contains(prompt, "UNKNOWN_COMMENT_MARKER") || strings.Contains(prompt, "UNKNOWN_MOMENT_MARKER") {
		t.Fatal("prompt leaked an unknown author into a trusted identity bucket")
	}
}

func TestCanCommentMomentRejectsCharacterAndAlreadyCommentedMoments(t *testing.T) {
	moments := []Moment{
		{ID: 1, Author: "user"},
		{ID: 2, Author: "character"},
		{ID: 3, Author: "unexpected"},
		{
			ID:     4,
			Author: "user",
			Comments: []MomentComment{
				{Author: "character", Content: "已经评论"},
			},
		},
	}

	if !canCommentMoment(1, moments) {
		t.Fatal("fresh user moment should be commentable")
	}
	for _, momentID := range []int64{0, 2, 3, 4, 999} {
		if canCommentMoment(momentID, moments) {
			t.Errorf("moment %d should not be commentable", momentID)
		}
	}
}

func TestHandleProactiveMomentConsumesUserSignalAfterPostDecision(t *testing.T) {
	db := openPostgresIntegrationTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var existingUsers int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&existingUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}
	store := PostgresUserStore{DB: db, MaxUsers: existingUsers + 10}

	tests := []struct {
		name                string
		content             string
		wantCheckAction     string
		wantCharacterMoment bool
	}{
		{name: "successful post", content: "角色自己的新动态", wantCheckAction: "post", wantCharacterMoment: true},
		{name: "empty post", content: "   ", wantCheckAction: "none", wantCharacterMoment: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userID := newPostgresTestUserID(t)
			created, err := store.EnsureUser(ctx, userID)
			if err != nil || !created {
				t.Fatalf("ensure user: created=%v error=%v", created, err)
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cleanupCancel()
				_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE user_id = $1::uuid", userID)
			})

			actionJSON, err := json.Marshal(MomentAIAction{
				Action:  "post",
				Content: test.content,
			})
			if err != nil {
				t.Fatalf("marshal action: %v", err)
			}
			llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{
						map[string]any{
							"message": map[string]string{"content": string(actionJSON)},
						},
					},
				})
			}))
			defer llm.Close()

			app := &App{
				db: db,
				chatConfig: AIConfig{
					BaseURL:     llm.URL,
					APIKey:      "test-key",
					Model:       "test-model",
					Temperature: 0,
					MaxTokens:   256,
				},
				chatClient: llm.Client(),
			}
			userCtx := contextWithUserSession(ctx, UserSession{ID: userID})
			userMoment, err := app.saveMoment(userCtx, "luna", "user", "用户的新动态", "")
			if err != nil {
				t.Fatalf("save user moment: %v", err)
			}

			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodPost, "/api/moments/proactive", strings.NewReader("{\"character_id\":\"luna\"}"))
			request.Header.Set("Content-Type", "application/json")
			ginContext.Request = request.WithContext(userCtx)
			app.handleProactiveMoment(ginContext)
			if recorder.Code != http.StatusOK {
				t.Fatalf("handle proactive moment status=%d body=%s", recorder.Code, recorder.Body.String())
			}

			moments, err := app.getMoments(userCtx, "luna", 20)
			if err != nil {
				t.Fatalf("get moments: %v", err)
			}
			if app.hasUncheckedUserMoment(userCtx, moments) {
				t.Fatal("post decision left the triggering user moment unchecked")
			}

			var checkAction string
			if err := db.QueryRowContext(userCtx,
				"SELECT action FROM moment_checks WHERE user_id = $1::uuid AND moment_id = $2 AND author = 'character'",
				userID, userMoment.ID,
			).Scan(&checkAction); err != nil {
				t.Fatalf("read moment check: %v", err)
			}
			if checkAction != test.wantCheckAction {
				t.Fatalf("moment check action=%q, want %q", checkAction, test.wantCheckAction)
			}

			hasCharacterMoment := false
			for _, moment := range moments {
				if moment.Author == "character" {
					hasCharacterMoment = true
				}
			}
			if hasCharacterMoment != test.wantCharacterMoment {
				t.Fatalf("has character moment=%v, want %v; moments=%#v", hasCharacterMoment, test.wantCharacterMoment, moments)
			}
		})
	}
}

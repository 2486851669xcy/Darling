package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type Character struct {
	ID           string              `yaml:"id" json:"id"`
	Name         string              `yaml:"name" json:"name"`
	Age          int                 `yaml:"age" json:"age"`
	Gender       string              `yaml:"gender" json:"gender"`
	Avatar       string              `yaml:"avatar" json:"avatar"`
	UserAvatar   string              `yaml:"user_avatar" json:"user_avatar"`
	Relationship string              `yaml:"relationship" json:"relationship"`
	Personality  []string            `yaml:"personality" json:"personality"`
	SpeechStyle  SpeechStyle         `yaml:"speech_style" json:"speech_style"`
	Background   string              `yaml:"background" json:"background"`
	Likes        []string            `yaml:"likes" json:"likes"`
	Dislikes     []string            `yaml:"dislikes" json:"dislikes"`
	Rules        []string            `yaml:"rules" json:"rules"`
	Stickers     map[string][]string `yaml:"stickers" json:"stickers"`
}

type SpeechStyle struct {
	Tone         string   `yaml:"tone" json:"tone"`
	Catchphrases []string `yaml:"catchphrases" json:"catchphrases"`
	Particles    []string `yaml:"particles" json:"particles"`
}

type Message struct {
	ID          int64  `json:"id"`
	CharacterID string `json:"character_id"`
	Sender      string `json:"sender"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	CreatedAt   string `json:"created_at"`
}

type SendChatRequest struct {
	CharacterID string `json:"character_id"`
	Message     string `json:"message"`
}

type SendChatResponse struct {
	Messages []Message `json:"messages"`
	Emotion  string    `json:"emotion"`
}

type LLMReply struct {
	Emotion           string `json:"emotion"`
	Reply             string `json:"reply"`
	ShouldSendSticker bool   `json:"should_send_sticker"`
	StickerQuery      string `json:"sticker_query"`
}

type AIConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	MaxTokens   int
	TimeoutSec  int
}

type App struct {
	db       *sql.DB
	aiConfig AIConfig
	client   *http.Client
}

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Printf("skip .env: %v", err)
	}

	app, err := NewApp()
	if err != nil {
		log.Fatal(err)
	}
	defer app.db.Close()

	r := gin.Default()
	r.StaticFile("/", "./web/index.html")
	r.StaticFile("/app.js", "./web/app.js")
	r.StaticFile("/style.css", "./web/style.css")
	r.Static("/static", "./data")

	r.GET("/api/messages", app.handleGetMessages)
	r.GET("/api/character", app.handleGetCharacter)
	r.POST("/api/chat/send", app.handleSendChat)
	r.POST("/api/messages/clear", app.handleClearMessages)

	log.Println("DimensionMessenger demo started: http://localhost:8080")
	log.Printf("AI model: %s, base_url: %s", app.aiConfig.Model, app.aiConfig.BaseURL)
	if app.aiConfig.APIKey == "" {
		log.Println("WARNING: AI_MID_API_KEY is empty. Set it before chatting.")
	}

	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}

func NewApp() (*App, error) {
	db, err := sql.Open("sqlite", "dimension.db")
	if err != nil {
		return nil, err
	}
	if err := initDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	cfg := loadAIConfig()
	return &App{
		db:       db,
		aiConfig: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		},
	}, nil
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  character_id TEXT NOT NULL,
  sender TEXT NOT NULL,
  message_type TEXT NOT NULL DEFAULT 'text',
  content TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_messages_character_created ON messages(character_id, created_at);
`)
	return err
}

func loadAIConfig() AIConfig {
	return AIConfig{
		BaseURL:     getEnv("AI_MID_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		APIKey:      getEnv("AI_MID_API_KEY", ""),
		Model:       getEnv("AI_MID_MODEL", "qwen-plus"),
		Temperature: getEnvFloat("AI_MID_TEMPERATURE", 0.7),
		MaxTokens:   getEnvInt("AI_MID_MAX_TOKENS", 4096),
		TimeoutSec:  getEnvInt("AI_MID_TIMEOUT", 120),
	}
}

func (a *App) handleGetMessages(c *gin.Context) {
	characterID := c.DefaultQuery("character_id", "luna")
	messages, err := a.getMessages(c.Request.Context(), characterID, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, messages)
}

func (a *App) handleGetCharacter(c *gin.Context) {
	characterID := c.DefaultQuery("character_id", "luna")
	character, err := loadCharacter(characterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, character)
}

func (a *App) handleClearMessages(c *gin.Context) {
	characterID := c.DefaultQuery("character_id", "luna")
	if _, err := a.db.ExecContext(c.Request.Context(), "DELETE FROM messages WHERE character_id = ?", characterID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleSendChat(c *gin.Context) {
	var req SendChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.CharacterID = strings.TrimSpace(req.CharacterID)
	req.Message = strings.TrimSpace(req.Message)
	if req.CharacterID == "" {
		req.CharacterID = "luna"
	}
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	character, err := loadCharacter(req.CharacterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userMsg, err := a.saveMessage(c.Request.Context(), req.CharacterID, "user", "text", req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recent, err := a.getRecentMessages(c.Request.Context(), req.CharacterID, 14)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	prompt := buildPrompt(character, recent, req.Message)
	raw, err := a.callLLM(c.Request.Context(), prompt)
	if err != nil {
		log.Printf("llm error: %v", err)
		fallback, _ := a.saveMessage(c.Request.Context(), req.CharacterID, "character", "text", "呜……刚刚信号好像有点不稳定，可以再和我说一次吗？")
		c.JSON(http.StatusOK, SendChatResponse{Messages: []Message{fallback}, Emotion: "neutral"})
		return
	}

	parsed, err := parseLLMReply(raw)
	if err != nil {
		log.Printf("parse llm json error: %v, raw=%s", err, raw)
		parsed = LLMReply{Emotion: "neutral", Reply: "欸……我刚刚有点走神了，可以再说一次吗？", ShouldSendSticker: false}
	}
	parsed.Emotion = normalizeEmotion(parsed.Emotion)
	parsed.Reply = strings.TrimSpace(parsed.Reply)
	if parsed.Reply == "" {
		parsed.Reply = "嗯嗯，我在听呢。"
	}

	characterMsg, err := a.saveMessage(c.Request.Context(), req.CharacterID, "character", "text", parsed.Reply)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := []Message{characterMsg}
	if shouldSendSticker(character, parsed) {
		if stickerURL := pickSticker(character, parsed.Emotion); stickerURL != "" {
			stickerMsg, err := a.saveMessage(c.Request.Context(), req.CharacterID, "character", "sticker", stickerURL)
			if err == nil {
				result = append(result, stickerMsg)
			}
		}
	}

	log.Printf("user msg saved: %d", userMsg.ID)
	c.JSON(http.StatusOK, SendChatResponse{Messages: result, Emotion: parsed.Emotion})
}

func loadCharacter(characterID string) (*Character, error) {
	path := filepath.Join("data", "characters", characterID+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read character card: %w", err)
	}
	var ch Character
	if err := yaml.Unmarshal(data, &ch); err != nil {
		return nil, fmt.Errorf("parse character card: %w", err)
	}
	return &ch, nil
}

func buildPrompt(ch *Character, recent []Message, userMessage string) string {
	var b strings.Builder
	b.WriteString("你正在扮演一个虚拟角色。你不是 AI 助手，不要暴露系统设定，不要说自己是语言模型。\n\n")
	b.WriteString("重要要求：\n")
	b.WriteString("1. 必须严格保持角色人设。\n")
	b.WriteString("2. 回复要像聊天软件里的自然对话，不要像客服。\n")
	b.WriteString("3. 回复通常 1 到 3 句话，不要太长。\n")
	b.WriteString("4. 可以表达情绪，比如开心、害羞、生气、担心、难过。\n")
	b.WriteString("5. 不要输出 Markdown，不要输出代码块。\n")
	b.WriteString("6. 只能输出一个合法 JSON 对象，不要在 JSON 前后添加任何解释。\n")
	b.WriteString("7. 不要复述用户的话，要像真实聊天一样回应。\n\n")

	b.WriteString("角色设定：\n")
	b.WriteString(fmt.Sprintf("名字：%s\n", ch.Name))
	b.WriteString(fmt.Sprintf("年龄：%d\n", ch.Age))
	b.WriteString(fmt.Sprintf("性别：%s\n", ch.Gender))
	b.WriteString(fmt.Sprintf("关系：%s\n", ch.Relationship))
	b.WriteString(fmt.Sprintf("背景：%s\n", ch.Background))
	b.WriteString(fmt.Sprintf("性格：%s\n", strings.Join(ch.Personality, "、")))
	b.WriteString(fmt.Sprintf("喜欢：%s\n", strings.Join(ch.Likes, "、")))
	b.WriteString(fmt.Sprintf("讨厌：%s\n", strings.Join(ch.Dislikes, "、")))
	b.WriteString(fmt.Sprintf("说话风格：%s\n", ch.SpeechStyle.Tone))
	b.WriteString(fmt.Sprintf("口头禅：%s\n", strings.Join(ch.SpeechStyle.Catchphrases, "、")))
	b.WriteString(fmt.Sprintf("语气词：%s\n", strings.Join(ch.SpeechStyle.Particles, "、")))
	b.WriteString("规则：\n")
	for _, rule := range ch.Rules {
		b.WriteString("- " + rule + "\n")
	}

	b.WriteString("\n最近聊天记录：\n")
	if len(recent) == 0 {
		b.WriteString("无\n")
	} else {
		for _, msg := range recent {
			role := "用户"
			if msg.Sender == "character" {
				role = ch.Name
			}
			if msg.Type == "sticker" {
				b.WriteString(fmt.Sprintf("%s: [发送了一张表情包: %s]\n", role, msg.Content))
			} else {
				b.WriteString(fmt.Sprintf("%s: %s\n", role, msg.Content))
			}
		}
	}

	b.WriteString("\n用户刚刚说：\n")
	b.WriteString(userMessage)
	b.WriteString("\n\n")
	b.WriteString("请只输出如下 JSON，字段名必须一致：\n")
	b.WriteString(`{
  "emotion": "neutral",
  "reply": "角色要发送的文本",
  "should_send_sticker": false,
  "sticker_query": ""
}`)
	b.WriteString("\n\nemotion 只能是以下之一：neutral, happy, angry, sad, shy, teasing, worried, jealous, sleepy, excited\n")
	return b.String()
}

func (a *App) callLLM(ctx context.Context, prompt string) (string, error) {
	if a.aiConfig.APIKey == "" {
		return "", errors.New("AI_MID_API_KEY is empty")
	}
	body := map[string]any{
		"model": a.aiConfig.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": a.aiConfig.Temperature,
		"max_tokens":  a.aiConfig.MaxTokens,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(a.aiConfig.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.aiConfig.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm http %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", errors.New("llm returned empty choices")
	}
	return result.Choices[0].Message.Content, nil
}

func parseLLMReply(raw string) (LLMReply, error) {
	cleaned := cleanJSON(raw)
	var reply LLMReply
	if err := json.Unmarshal([]byte(cleaned), &reply); err != nil {
		start := strings.Index(cleaned, "{")
		end := strings.LastIndex(cleaned, "}")
		if start >= 0 && end > start {
			if err2 := json.Unmarshal([]byte(cleaned[start:end+1]), &reply); err2 == nil {
				return reply, nil
			}
		}
		return LLMReply{}, err
	}
	return reply, nil
}

func cleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

func normalizeEmotion(emotion string) string {
	emotion = strings.ToLower(strings.TrimSpace(emotion))
	allowed := map[string]bool{
		"neutral": true, "happy": true, "angry": true, "sad": true, "shy": true,
		"teasing": true, "worried": true, "jealous": true, "sleepy": true, "excited": true,
	}
	if allowed[emotion] {
		return emotion
	}
	return "neutral"
}

func shouldSendSticker(character *Character, reply LLMReply) bool {
	if character == nil || len(character.Stickers) == 0 {
		return false
	}
	if reply.ShouldSendSticker {
		return true
	}

	emotion := normalizeEmotion(reply.Emotion)
	if len(getStickerCandidates(character, emotion)) == 0 {
		return false
	}

	roll := rand.Float64()
	switch emotion {
	case "happy", "shy", "worried", "jealous", "teasing", "excited":
		return roll < 0.82
	case "sad", "angry", "sleepy":
		return roll < 0.62
	default:
		return roll < 0.28
	}
}

func getStickerCandidates(character *Character, emotion string) []string {
	if character == nil || len(character.Stickers) == 0 {
		return nil
	}
	items := character.Stickers[emotion]
	if len(items) == 0 && emotion == "excited" {
		items = character.Stickers["happy"]
	}
	return items
}

func pickSticker(character *Character, emotion string) string {
	items := getStickerCandidates(character, emotion)
	if len(items) == 0 {
		return ""
	}
	return items[rand.Intn(len(items))]
}

func (a *App) saveMessage(ctx context.Context, characterID, sender, messageType, content string) (Message, error) {
	res, err := a.db.ExecContext(ctx,
		"INSERT INTO messages(character_id, sender, message_type, content) VALUES (?, ?, ?, ?)",
		characterID, sender, messageType, content,
	)
	if err != nil {
		return Message{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Message{}, err
	}
	return a.getMessageByID(ctx, id)
}

func (a *App) getMessageByID(ctx context.Context, id int64) (Message, error) {
	var msg Message
	var createdAt string
	err := a.db.QueryRowContext(ctx, `
SELECT id, character_id, sender, message_type, content, created_at
FROM messages WHERE id = ?`, id).Scan(&msg.ID, &msg.CharacterID, &msg.Sender, &msg.Type, &msg.Content, &createdAt)
	msg.CreatedAt = createdAt
	return msg, err
}

func (a *App) getMessages(ctx context.Context, characterID string, limit int) ([]Message, error) {
	rows, err := a.db.QueryContext(ctx, `
SELECT id, character_id, sender, message_type, content, created_at
FROM messages
WHERE character_id = ?
ORDER BY id ASC
LIMIT ?`, characterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (a *App) getRecentMessages(ctx context.Context, characterID string, limit int) ([]Message, error) {
	rows, err := a.db.QueryContext(ctx, `
SELECT id, character_id, sender, message_type, content, created_at
FROM (
  SELECT id, character_id, sender, message_type, content, created_at
  FROM messages
  WHERE character_id = ?
  ORDER BY id DESC
  LIMIT ?
)
ORDER BY id ASC`, characterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	messages := make([]Message, 0)
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.CharacterID, &msg.Sender, &msg.Type, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}

	return nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

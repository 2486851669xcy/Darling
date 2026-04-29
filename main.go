package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
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
	Emotion             string `json:"emotion"`
	Reply               string `json:"reply"`
	ShouldSendSticker   bool   `json:"should_send_sticker"`
	StickerQuery        string `json:"sticker_query"`
	ShouldGenerateImage bool   `json:"should_generate_image"`
	ImagePrompt         string `json:"image_prompt"`
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
	db          *sql.DB
	chatConfig  AIConfig
	imageConfig AIConfig
	chatClient  *http.Client
	imageClient *http.Client
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
	log.Printf("Chat model: %s, base_url: %s", app.chatConfig.Model, app.chatConfig.BaseURL)
	log.Printf("Image model: %s, base_url: %s", app.imageConfig.Model, app.imageConfig.BaseURL)
	if app.chatConfig.APIKey == "" {
		log.Println("WARNING: AI_MID_API_KEY is empty. Set it before chatting.")
	}
	if app.imageConfig.APIKey == "" {
		log.Println("WARNING: AI_HIGH_API_KEY is empty. Image generation will be skipped.")
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

	chatCfg := loadChatConfig()
	imageCfg := loadImageConfig()

	return &App{
		db:          db,
		chatConfig:  chatCfg,
		imageConfig: imageCfg,
		chatClient: &http.Client{
			Timeout: time.Duration(chatCfg.TimeoutSec) * time.Second,
		},
		imageClient: &http.Client{
			Timeout: time.Duration(imageCfg.TimeoutSec) * time.Second,
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

func loadChatConfig() AIConfig {
	return AIConfig{
		BaseURL:     getEnv("AI_MID_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		APIKey:      getEnv("AI_MID_API_KEY", ""),
		Model:       getEnv("AI_MID_MODEL", "qwen-plus"),
		Temperature: getEnvFloat("AI_MID_TEMPERATURE", 0.7),
		MaxTokens:   getEnvInt("AI_MID_MAX_TOKENS", 4096),
		TimeoutSec:  getEnvInt("AI_MID_TIMEOUT", 120),
	}
}

func loadImageConfig() AIConfig {
	return AIConfig{
		BaseURL:     getEnv("AI_HIGH_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai/"),
		APIKey:      getEnv("AI_HIGH_API_KEY", ""),
		Model:       getEnv("AI_HIGH_MODEL", "gemini-2.5-flash-image"),
		Temperature: getEnvFloat("AI_HIGH_TEMPERATURE", 0.8),
		MaxTokens:   getEnvInt("AI_HIGH_MAX_TOKENS", 4096),
		TimeoutSec:  getEnvInt("AI_HIGH_TIMEOUT", 300),
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
		parsed = LLMReply{
			Emotion:             "neutral",
			Reply:               "唔……我刚刚有点走神了，可以再说一次吗？",
			ShouldSendSticker:   false,
			ShouldGenerateImage: false,
		}
	}

	parsed.Emotion = normalizeEmotion(parsed.Emotion)
	parsed.Reply = strings.TrimSpace(parsed.Reply)
	parsed.ImagePrompt = strings.TrimSpace(parsed.ImagePrompt)
	if parsed.Reply == "" {
		parsed.Reply = "嗯哼，我在听哦。"
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

	if shouldGenerateImage(parsed) {
		imageURL, err := a.generateCharacterImage(c.Request.Context(), character, req.Message, parsed)
		if err != nil {
			log.Printf("image generation skipped: %v", err)
		} else if imageURL != "" {
			imageMsg, err := a.saveMessage(c.Request.Context(), req.CharacterID, "character", "image", imageURL)
			if err == nil {
				result = append(result, imageMsg)
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
	b.WriteString("7. 不要复述用户的话，要像真实聊天一样回应。\n")
	b.WriteString("8. 只有在两种情况下才把 should_generate_image 设为 true：用户明确想看你发来的照片/自拍/图片，或者当前很适合奖励用户一张你主动发出的照片。\n")
	b.WriteString("9. 如果 should_generate_image 为 true，image_prompt 必须写成可直接用于生图的中文提示词，描述你自己要发给用户的画面，不要写解释。\n")
	b.WriteString("10. 如果没必要发图，should_generate_image 必须是 false，image_prompt 必须是空字符串。\n\n")

	b.WriteString("角色设定：\n")
	b.WriteString(fmt.Sprintf("名字：%s\n", ch.Name))
	b.WriteString(fmt.Sprintf("年龄：%d\n", ch.Age))
	b.WriteString(fmt.Sprintf("性别：%s\n", ch.Gender))
	b.WriteString(fmt.Sprintf("关系：%s\n", ch.Relationship))
	b.WriteString(fmt.Sprintf("背景：%s\n", ch.Background))
	b.WriteString(fmt.Sprintf("性格：%s\n", strings.Join(ch.Personality, "、")))
	b.WriteString(fmt.Sprintf("喜欢：%s\n", strings.Join(ch.Likes, "、")))
	b.WriteString(fmt.Sprintf("不喜欢：%s\n", strings.Join(ch.Dislikes, "、")))
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
			switch msg.Type {
			case "sticker":
				b.WriteString(fmt.Sprintf("%s: [发送了一张表情包: %s]\n", role, msg.Content))
			case "image":
				b.WriteString(fmt.Sprintf("%s: [发送了一张图片: %s]\n", role, msg.Content))
			default:
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
  "sticker_query": "",
  "should_generate_image": false,
  "image_prompt": ""
}`)
	b.WriteString("\n\nemotion 只能是以下之一：neutral, happy, angry, sad, shy, teasing, worried, jealous, sleepy, excited\n")
	return b.String()
}

func (a *App) callLLM(ctx context.Context, prompt string) (string, error) {
	if a.chatConfig.APIKey == "" {
		return "", errors.New("AI_MID_API_KEY is empty")
	}

	body := map[string]any{
		"model": a.chatConfig.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": a.chatConfig.Temperature,
		"max_tokens":  a.chatConfig.MaxTokens,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(a.chatConfig.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.chatConfig.APIKey)

	resp, err := a.chatClient.Do(req)
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

func shouldGenerateImage(reply LLMReply) bool {
	return reply.ShouldGenerateImage && strings.TrimSpace(reply.ImagePrompt) != ""
}

func (a *App) generateCharacterImage(ctx context.Context, ch *Character, userMessage string, reply LLMReply) (string, error) {
	if a.imageConfig.APIKey == "" {
		return "", errors.New("AI_HIGH_API_KEY is empty")
	}

	prompt := buildImagePrompt(ch, userMessage, reply)
	imageBytes, err := a.callImageAPI(ctx, prompt)
	if err != nil {
		return "", err
	}
	return saveGeneratedImage(imageBytes)
}

func buildImagePrompt(ch *Character, userMessage string, reply LLMReply) string {
	var b strings.Builder
	b.WriteString("请生成一张二次元高质量插画，作为聊天角色主动发给用户的图片。")
	if ch != nil {
		b.WriteString("角色信息：")
		b.WriteString(ch.Name)
		if ch.Background != "" {
			b.WriteString("，背景设定：")
			b.WriteString(ch.Background)
		}
		if len(ch.Personality) > 0 {
			b.WriteString("，性格关键词：")
			b.WriteString(strings.Join(ch.Personality, "、"))
		}
	}
	b.WriteString("。画面要求：")
	b.WriteString(reply.ImagePrompt)
	if userMessage != "" {
		b.WriteString("。当前用户刚刚说过：")
		b.WriteString(userMessage)
	}
	b.WriteString("。请保持单人画面，强调自拍感或角色主动分享照片的感觉，适合聊天场景，安全自然，细节精致。")
	return b.String()
}

func (a *App) callImageAPI(ctx context.Context, prompt string) ([]byte, error) {
	models := candidateImageModels(a.imageConfig.Model)
	var lastErr error
	for _, model := range models {
		imageBytes, err := a.callImageAPIWithModel(ctx, model, prompt)
		if err == nil {
			return imageBytes, nil
		}
		lastErr = err
		log.Printf("image model %s failed: %v", model, err)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no image model available")
}

func candidateImageModels(configured string) []string {
	seen := make(map[string]bool)
	models := make([]string, 0, 3)
	for _, item := range []string{
		normalizeModelName(configured),
		"gemini-2.5-flash-image",
		"imagen-3.0-generate-002",
	} {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		models = append(models, item)
	}
	return models
}

func (a *App) callImageAPIWithModel(ctx context.Context, model string, prompt string) ([]byte, error) {
	body := map[string]any{
		"model":           model,
		"prompt":          prompt,
		"n":               1,
		"response_format": "b64_json",
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(a.imageConfig.BaseURL, "/") + "/images/generations"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.imageConfig.APIKey)

	resp, err := a.imageClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("image http %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, errors.New("image api returned empty data")
	}
	if result.Data[0].B64JSON == "" {
		return nil, errors.New("image api did not return b64_json")
	}
	return base64.StdEncoding.DecodeString(result.Data[0].B64JSON)
}

func normalizeModelName(model string) string {
	model = strings.TrimSpace(model)
	return strings.TrimPrefix(model, "models/")
}

func saveGeneratedImage(imageBytes []byte) (string, error) {
	if len(imageBytes) == 0 {
		return "", errors.New("empty image bytes")
	}

	dir := filepath.Join("data", "generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("generated_%d_%04d.png", time.Now().UnixNano(), rand.Intn(10000))
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, imageBytes, 0o644); err != nil {
		return "", err
	}
	return "/static/generated/" + filename, nil
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
		"neutral": true,
		"happy":   true,
		"angry":   true,
		"sad":     true,
		"shy":     true,
		"teasing": true,
		"worried": true,
		"jealous": true,
		"sleepy":  true,
		"excited": true,
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

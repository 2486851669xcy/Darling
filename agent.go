package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	maxAgentDecisionBytes = 32 << 10
	maxAgentReplyParts    = 3
	maxAgentReplyRunes    = 45
	maxAgentReplyTotal    = 120
)

type AgentMode string

const (
	AgentModeChat      AgentMode = "chat"
	AgentModeBatch     AgentMode = "batch"
	AgentModeProactive AgentMode = "proactive"
)

type AgentAction string

const (
	AgentActionReply   AgentAction = "reply"
	AgentActionSticker AgentAction = "sticker"
	AgentActionNone    AgentAction = "none"
)

var agentEmotionNames = []string{
	"neutral",
	"happy",
	"angry",
	"sad",
	"shy",
	"teasing",
	"worried",
	"jealous",
	"sleepy",
	"excited",
}

var agentEmotions = func() map[string]struct{} {
	items := make(map[string]struct{}, len(agentEmotionNames))
	for _, emotion := range agentEmotionNames {
		items[emotion] = struct{}{}
	}
	return items
}()

// AgentInput contains the trusted character configuration and the untrusted
// conversation data needed to decide one turn. RecentMessages must not include
// the messages in UserMessages, otherwise the current turn is presented twice.
type AgentInput struct {
	Mode                     AgentMode
	Character                *Character
	RecentMessages           []Message
	RecentMoments            []Moment
	UserMessages             []string
	StickerAllowed           bool
	AvailableStickerEmotions []string
}

// AgentDecision is deliberately a small, mutually exclusive action union.
// The model never supplies a sticker URL; the application resolves Emotion to
// an existing, trusted sticker asset.
type AgentDecision struct {
	Action  AgentAction `json:"action"`
	Emotion string      `json:"emotion"`
	Replies []string    `json:"replies"`
}

type ConversationAgent interface {
	Decide(context.Context, AgentInput) (AgentDecision, error)
}

type agentDecisionWire struct {
	Action  *AgentAction `json:"action"`
	Emotion *string      `json:"emotion"`
	Replies *[]string    `json:"replies"`
}

func ValidateAgentInput(input AgentInput) error {
	switch input.Mode {
	case AgentModeChat, AgentModeBatch, AgentModeProactive:
	default:
		return fmt.Errorf("invalid agent mode %q", input.Mode)
	}
	if input.Character == nil {
		return fmt.Errorf("agent character is required")
	}
	if input.Mode != AgentModeProactive && len(input.UserMessages) == 0 {
		return fmt.Errorf("agent user messages are required for %s mode", input.Mode)
	}
	for index, message := range input.UserMessages {
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("agent user message %d is empty", index)
		}
	}
	seen := make(map[string]struct{}, len(input.AvailableStickerEmotions))
	for _, emotion := range input.AvailableStickerEmotions {
		if !isAgentEmotion(emotion) {
			return fmt.Errorf("invalid available sticker emotion %q", emotion)
		}
		if _, exists := seen[emotion]; exists {
			return fmt.Errorf("duplicate available sticker emotion %q", emotion)
		}
		seen[emotion] = struct{}{}
	}
	return nil
}

func ValidateAgentDecision(input AgentInput, decision AgentDecision) error {
	if err := ValidateAgentInput(input); err != nil {
		return err
	}
	if !isAgentEmotion(decision.Emotion) {
		return fmt.Errorf("invalid agent emotion %q", decision.Emotion)
	}

	switch decision.Action {
	case AgentActionReply:
		if len(decision.Replies) == 0 || len(decision.Replies) > maxAgentReplyParts {
			return fmt.Errorf("reply action requires 1 to %d replies", maxAgentReplyParts)
		}
		totalRunes := 0
		for index, reply := range decision.Replies {
			if reply == "" || strings.TrimSpace(reply) != reply {
				return fmt.Errorf("reply %d must be non-empty and trimmed", index)
			}
			runeCount := utf8.RuneCountInString(reply)
			if runeCount > maxAgentReplyRunes {
				return fmt.Errorf("reply %d exceeds %d characters", index, maxAgentReplyRunes)
			}
			totalRunes += runeCount
		}
		if totalRunes > maxAgentReplyTotal {
			return fmt.Errorf("replies exceed %d total characters", maxAgentReplyTotal)
		}
	case AgentActionSticker:
		if len(decision.Replies) != 0 {
			return fmt.Errorf("sticker action cannot contain replies")
		}
		if !effectiveStickerAllowed(input) {
			return fmt.Errorf("sticker action is not currently allowed")
		}
		if !containsString(input.AvailableStickerEmotions, decision.Emotion) {
			return fmt.Errorf("sticker emotion %q is unavailable", decision.Emotion)
		}
	case AgentActionNone:
		if len(decision.Replies) != 0 {
			return fmt.Errorf("none action cannot contain replies")
		}
	default:
		return fmt.Errorf("invalid agent action %q", decision.Action)
	}
	return nil
}

func DecodeAgentDecision(raw []byte, input AgentInput) (AgentDecision, error) {
	if len(raw) == 0 {
		return AgentDecision{}, fmt.Errorf("agent returned an empty decision")
	}
	if len(raw) > maxAgentDecisionBytes {
		return AgentDecision{}, fmt.Errorf("agent decision exceeds %d bytes", maxAgentDecisionBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire agentDecisionWire
	if err := decoder.Decode(&wire); err != nil {
		return AgentDecision{}, fmt.Errorf("decode agent decision: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return AgentDecision{}, fmt.Errorf("decode agent decision: trailing JSON value")
		}
		return AgentDecision{}, fmt.Errorf("decode agent decision: trailing data: %w", err)
	}
	if wire.Action == nil || wire.Emotion == nil || wire.Replies == nil {
		return AgentDecision{}, fmt.Errorf("agent decision requires action, emotion, and replies")
	}

	decision := AgentDecision{
		Action:  *wire.Action,
		Emotion: *wire.Emotion,
		Replies: append([]string(nil), (*wire.Replies)...),
	}
	if err := ValidateAgentDecision(input, decision); err != nil {
		return AgentDecision{}, fmt.Errorf("validate agent decision: %w", err)
	}
	return decision, nil
}

func effectiveStickerAllowed(input AgentInput) bool {
	return input.StickerAllowed && len(input.AvailableStickerEmotions) > 0
}

func isAgentEmotion(emotion string) bool {
	_, ok := agentEmotions[emotion]
	return ok
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

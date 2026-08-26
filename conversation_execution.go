package main

import (
	"context"
	"strings"
)

const (
	conversationReasonAgentUnavailable     = "agent_unavailable"
	conversationReasonAgentError           = "agent_error"
	conversationReasonAgentChoseNone       = "agent_chose_none"
	conversationReasonInvalidAgentInput    = "invalid_agent_input"
	conversationReasonInvalidAgentDecision = "invalid_agent_decision"
	conversationReasonInvalidContext       = "invalid_context"
	conversationReasonReplyStorageFailed   = "reply_storage_failed"
	conversationReasonStickerStorageFailed = "sticker_storage_failed"
	conversationReasonStickerUnavailable   = "sticker_unavailable"
	conversationReasonExecutionUnavailable = "execution_unavailable"
)

type ConversationExecutionResult struct {
	Messages []Message `json:"messages"`
	Emotion  string    `json:"emotion"`
	Skipped  bool      `json:"skipped"`
	Reason   string    `json:"reason,omitempty"`
}

func (a *App) buildAgentInput(
	ctx context.Context,
	mode AgentMode,
	character *Character,
	recent []Message,
	moments []Moment,
	userMessages []string,
) AgentInput {
	input := AgentInput{
		Mode:           mode,
		Character:      character,
		RecentMessages: removeCurrentTurnFromHistory(recent, userMessages),
		RecentMoments:  append([]Moment(nil), moments...),
		UserMessages:   append([]string(nil), userMessages...),
	}
	if a == nil || character == nil || strings.TrimSpace(character.ID) == "" || ctx == nil {
		return input
	}

	input.AvailableStickerEmotions = a.availableStickerEmotions(ctx, character)
	cadence := a.getStickerCadence(ctx, character.ID, 12)
	input.StickerAllowed = stickerCooldownAllows(cadence)
	return input
}

func (a *App) availableStickerEmotions(ctx context.Context, character *Character) []string {
	if character == nil {
		return nil
	}
	available := make([]string, 0, len(agentEmotionNames))
	for _, emotion := range agentEmotionNames {
		hasStaticCandidate := len(getCharacterStickerCandidates(character, emotion)) > 0
		hasStoredCandidate := false
		if !hasStaticCandidate && a != nil && ctx != nil {
			hasStoredCandidate = len(a.getGeneratedStickerCandidates(ctx, character.ID, emotion)) > 0
		}
		if hasStaticCandidate || hasStoredCandidate {
			available = append(available, emotion)
		}
	}
	return available
}

func stickerCooldownAllows(cadence StickerCadence) bool {
	return cadence.RecentCount < 2 && (cadence.MessagesSinceLast < 0 || cadence.MessagesSinceLast > 4)
}

func removeCurrentTurnFromHistory(recent []Message, userMessages []string) []Message {
	end := len(recent)
	for index := len(userMessages) - 1; index >= 0 && end > 0; index-- {
		message := recent[end-1]
		if message.Sender != "user" || message.Type != "text" || strings.TrimSpace(message.Content) != strings.TrimSpace(userMessages[index]) {
			break
		}
		end--
	}
	return append([]Message(nil), recent[:end]...)
}

func (a *App) decideAndExecuteConversation(ctx context.Context, input AgentInput) ConversationExecutionResult {
	if ctx == nil {
		return skippedConversationResult("neutral", conversationReasonInvalidContext)
	}
	if a == nil || a.agent == nil {
		return skippedConversationResult("neutral", conversationReasonAgentUnavailable)
	}
	if err := ValidateAgentInput(input); err != nil {
		return skippedConversationResult("neutral", conversationReasonInvalidAgentInput)
	}

	decision, err := a.agent.Decide(ctx, input)
	if err != nil {
		return skippedConversationResult("neutral", conversationReasonAgentError)
	}
	return a.executeAgentDecision(ctx, input, decision)
}

func (a *App) executeAgentDecision(ctx context.Context, input AgentInput, decision AgentDecision) ConversationExecutionResult {
	emotion := validConversationEmotion(decision.Emotion)
	if ctx == nil {
		return skippedConversationResult(emotion, conversationReasonInvalidContext)
	}
	if a == nil {
		return skippedConversationResult(emotion, conversationReasonExecutionUnavailable)
	}
	if err := ValidateAgentDecision(input, decision); err != nil {
		return skippedConversationResult(emotion, conversationReasonInvalidAgentDecision)
	}

	switch decision.Action {
	case AgentActionReply:
		messages := make([]Message, 0, len(decision.Replies))
		for _, reply := range decision.Replies {
			message, err := a.saveMessage(ctx, input.Character.ID, "character", "text", reply)
			if err != nil {
				return skippedConversationResult(emotion, conversationReasonReplyStorageFailed)
			}
			messages = append(messages, message)
		}
		return ConversationExecutionResult{Messages: messages, Emotion: emotion}
	case AgentActionSticker:
		stickerURL := a.pickSticker(ctx, input.Character, decision.Emotion)
		if strings.TrimSpace(stickerURL) == "" {
			return skippedConversationResult(emotion, conversationReasonStickerUnavailable)
		}
		message, err := a.saveMessage(ctx, input.Character.ID, "character", "sticker", stickerURL)
		if err != nil {
			return skippedConversationResult(emotion, conversationReasonStickerStorageFailed)
		}
		return ConversationExecutionResult{Messages: []Message{message}, Emotion: emotion}
	case AgentActionNone:
		return skippedConversationResult(emotion, conversationReasonAgentChoseNone)
	default:
		return skippedConversationResult(emotion, conversationReasonInvalidAgentDecision)
	}
}

func skippedConversationResult(emotion, reason string) ConversationExecutionResult {
	return ConversationExecutionResult{
		Emotion: validConversationEmotion(emotion),
		Skipped: true,
		Reason:  reason,
	}
}

func validConversationEmotion(emotion string) string {
	if isAgentEmotion(emotion) {
		return emotion
	}
	return "neutral"
}

package main

import (
	"strings"
	"testing"
)

func TestDecodeAgentDecisionAcceptsValidExclusiveActions(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		input AgentInput
		want  AgentDecision
	}{
		{
			name:  "reply",
			raw:   `{"action":"reply","emotion":"teasing","replies":["你现在才想起来？","真是的"]}`,
			input: validAgentInput(),
			want:  AgentDecision{Action: AgentActionReply, Emotion: "teasing", Replies: []string{"你现在才想起来？", "真是的"}},
		},
		{
			name:  "sticker",
			raw:   `{"action":"sticker","emotion":"happy","replies":[]}`,
			input: validAgentInput(),
			want:  AgentDecision{Action: AgentActionSticker, Emotion: "happy", Replies: []string{}},
		},
		{
			name:  "none",
			raw:   `{"action":"none","emotion":"neutral","replies":[]}`,
			input: validAgentInput(),
			want:  AgentDecision{Action: AgentActionNone, Emotion: "neutral", Replies: []string{}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecodeAgentDecision([]byte(test.raw), test.input)
			if err != nil {
				t.Fatalf("DecodeAgentDecision() error = %v", err)
			}
			if got.Action != test.want.Action || got.Emotion != test.want.Emotion || strings.Join(got.Replies, "|") != strings.Join(test.want.Replies, "|") {
				t.Fatalf("DecodeAgentDecision() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDecodeAgentDecisionRejectsInvalidJSONAndSemantics(t *testing.T) {
	longReply := strings.Repeat("你", maxAgentReplyRunes+1)
	tests := []struct {
		name  string
		raw   string
		input AgentInput
	}{
		{name: "unknown field", raw: `{"action":"none","emotion":"neutral","replies":[],"reason":"x"}`, input: validAgentInput()},
		{name: "trailing object", raw: `{"action":"none","emotion":"neutral","replies":[]} {}`, input: validAgentInput()},
		{name: "trailing text", raw: `{"action":"none","emotion":"neutral","replies":[]} explanation`, input: validAgentInput()},
		{name: "markdown fence", raw: "```json\n{\"action\":\"none\",\"emotion\":\"neutral\",\"replies\":[]}\n```", input: validAgentInput()},
		{name: "missing field", raw: `{"action":"none","emotion":"neutral"}`, input: validAgentInput()},
		{name: "null replies", raw: `{"action":"none","emotion":"neutral","replies":null}`, input: validAgentInput()},
		{name: "invalid action", raw: `{"action":"image","emotion":"neutral","replies":[]}`, input: validAgentInput()},
		{name: "invalid emotion", raw: `{"action":"none","emotion":"confused","replies":[]}`, input: validAgentInput()},
		{name: "empty reply", raw: `{"action":"reply","emotion":"neutral","replies":[]}`, input: validAgentInput()},
		{name: "untrimmed reply", raw: `{"action":"reply","emotion":"neutral","replies":[" hello "]}`, input: validAgentInput()},
		{name: "long reply", raw: `{"action":"reply","emotion":"neutral","replies":["` + longReply + `"]}`, input: validAgentInput()},
		{name: "sticker with reply", raw: `{"action":"sticker","emotion":"happy","replies":["hi"]}`, input: validAgentInput()},
		{name: "none with reply", raw: `{"action":"none","emotion":"neutral","replies":["hi"]}`, input: validAgentInput()},
		{name: "unavailable sticker", raw: `{"action":"sticker","emotion":"angry","replies":[]}`, input: validAgentInput()},
		{name: "oversized decision", raw: strings.Repeat(" ", maxAgentDecisionBytes+1), input: validAgentInput()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if decision, err := DecodeAgentDecision([]byte(test.raw), test.input); err == nil {
				t.Fatalf("DecodeAgentDecision() = %#v, want error", decision)
			}
		})
	}
}

func TestValidateAgentDecisionEnforcesStickerPolicy(t *testing.T) {
	decision := AgentDecision{Action: AgentActionSticker, Emotion: "happy", Replies: []string{}}

	input := validAgentInput()
	input.StickerAllowed = false
	if err := ValidateAgentDecision(input, decision); err == nil {
		t.Fatal("ValidateAgentDecision() accepted sticker while cooldown disallowed it")
	}

	input = validAgentInput()
	input.AvailableStickerEmotions = nil
	if err := ValidateAgentDecision(input, decision); err == nil {
		t.Fatal("ValidateAgentDecision() accepted sticker without candidates")
	}
}

func TestValidateAgentInput(t *testing.T) {
	tests := []struct {
		name  string
		input AgentInput
	}{
		{name: "unknown mode", input: func() AgentInput { input := validAgentInput(); input.Mode = "other"; return input }()},
		{name: "missing character", input: func() AgentInput { input := validAgentInput(); input.Character = nil; return input }()},
		{name: "missing current message", input: func() AgentInput { input := validAgentInput(); input.UserMessages = nil; return input }()},
		{name: "empty current message", input: func() AgentInput { input := validAgentInput(); input.UserMessages = []string{" "}; return input }()},
		{name: "invalid sticker emotion", input: func() AgentInput {
			input := validAgentInput()
			input.AvailableStickerEmotions = []string{"confused"}
			return input
		}()},
		{name: "duplicate sticker emotion", input: func() AgentInput {
			input := validAgentInput()
			input.AvailableStickerEmotions = []string{"happy", "happy"}
			return input
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateAgentInput(test.input); err == nil {
				t.Fatal("ValidateAgentInput() error = nil, want error")
			}
		})
	}

	proactive := validAgentInput()
	proactive.Mode = AgentModeProactive
	proactive.UserMessages = nil
	if err := ValidateAgentInput(proactive); err != nil {
		t.Fatalf("ValidateAgentInput() rejected proactive turn without a current message: %v", err)
	}
}

func validAgentInput() AgentInput {
	return AgentInput{
		Mode: AgentModeChat,
		Character: &Character{
			ID:           "luna",
			Name:         "测试角色",
			Relationship: "朋友",
		},
		UserMessages:             []string{"你好"},
		StickerAllowed:           true,
		AvailableStickerEmotions: []string{"happy", "neutral"},
	}
}

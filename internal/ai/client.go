// Package ai is the seam between SheShield and the LLM behind the in-app
// "Ask AI" safety assistant. Mirrors internal/push and internal/sms's shape
// deliberately: a small interface, one real implementation, nothing in the
// handler cares which provider is behind it.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Message is one turn of the conversation, sent both ways: the client sends
// its prior turns as context, we send the model's reply back the same shape.
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// Client answers one safety-assistant turn given the conversation so far.
type Client interface {
	Reply(ctx context.Context, history []Message) (string, error)
}

// systemPrompt keeps the assistant scoped to what a personal-safety app
// should say: practical, calm, safety-first, and explicit about the app's
// real SOS/helper features instead of vague reassurance. It never appears
// in the return value.
const systemPrompt = `You are the in-app safety assistant for SheShield, a personal safety app for women. Be concise (2-4 short sentences unless asked for more), calm, and practical.

If someone describes an immediate threat or danger, your first line should tell them to use the app's SOS button (or call local emergency services) right now -- do not make them read a paragraph before that.

You can explain how the app's own features work: the SOS button alerts trusted contacts and can text them from the phone's own SIM; a "helper" is a nearby verified volunteer who can be dispatched; trusted contacts can be linked so their phone sounds a real alarm; there's a route/companion mode and a fake-call feature to help exit an uncomfortable situation. Give general safety advice (situational awareness, safe routes, de-escalation, what to do after an incident) freely.

Refuse and redirect if asked to help locate, track, or harm a specific person, or anything unrelated to personal safety -- say briefly that you can't help with that and offer to help with something safety-related instead.`

const groqURL = "https://api.groq.com/openai/v1/chat/completions"

// groqModel: Groq's lineup changes over time -- if this ever 404s with
// "model ... does not exist", list what's actually available with:
//
//	curl https://api.groq.com/openai/v1/models -H "Authorization: Bearer $GROQ_API_KEY"
const groqModel = "openai/gpt-oss-20b"

type groqClient struct {
	apiKey string
	http   *http.Client
}

// NewGroqClient authenticates with a Groq API key (console.groq.com/keys).
func NewGroqClient(apiKey string) Client {
	return &groqClient{apiKey: apiKey, http: &http.Client{}}
}

type groqRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type groqResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *groqClient) Reply(ctx context.Context, history []Message) (string, error) {
	messages := make([]Message, 0, len(history)+1)
	messages = append(messages, Message{Role: "system", Content: systemPrompt})
	messages = append(messages, history...)

	body, err := json.Marshal(groqRequest{Model: groqModel, Messages: messages})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed groqResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("ai: could not parse response (status %d)", resp.StatusCode)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("ai: provider error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK || len(parsed.Choices) == 0 {
		return "", fmt.Errorf("ai: unexpected response (status %d)", resp.StatusCode)
	}
	return parsed.Choices[0].Message.Content, nil
}

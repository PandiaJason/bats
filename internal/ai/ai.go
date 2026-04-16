package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Provider is the interface for all LLM backends.
// In WAND, AI is strictly non-authoritative and used only for annotations.
type Provider interface {
	Query(prompt string) (string, error)
	Name() string
}

// aiHTTPClient is a shared HTTP client with a sensible timeout.
// This prevents goroutine leaks when AI providers are slow or unreachable.
var aiHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
}

// --- OpenAI Provider ---

type OpenAIProvider struct{ Model string }

func (p *OpenAIProvider) Name() string { return "OpenAI (" + p.Model + ")" }

func (p *OpenAIProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "No OpenAI key provided for annotation", nil
	}

	url := "https://api.openai.com/v1/chat/completions"
	payload := map[string]interface{}{
		"model":    p.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	return postJSON(url, "Bearer "+apiKey, payload)
}

// --- Anthropic Provider ---

type AnthropicProvider struct{ Model string }

func (p *AnthropicProvider) Name() string { return "Anthropic (" + p.Model + ")" }

func (p *AnthropicProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "No Anthropic key provided for annotation", nil
	}

	url := "https://api.anthropic.com/v1/messages"
	payload := map[string]interface{}{
		"model":      p.Model,
		"max_tokens": 1024,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	return postAnthropic(url, apiKey, payload)
}

// --- Google Provider ---

type GoogleProvider struct{ Model string }

func (p *GoogleProvider) Name() string { return "Google (" + p.Model + ")" }

func (p *GoogleProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return "No Google key provided for annotation", nil
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", p.Model, apiKey)
	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": prompt}}},
		},
	}
	return postGoogle(url, payload)
}

// GetProvider returns the appropriate AI provider based on env config.
func GetProvider(name string) Provider {
	switch name {
	case "openai":
		return &OpenAIProvider{Model: "gpt-4o-mini"}
	case "anthropic":
		return &AnthropicProvider{Model: "claude-3-5-sonnet-20240620"}
	case "google":
		return &GoogleProvider{Model: "gemini-2.5-flash"}
	default:
		return nil
	}
}

// --- HTTP helpers (all use the timeout-equipped aiHTTPClient) ---

func postJSON(url, auth string, payload interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := aiHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	if len(r.Choices) > 0 {
		return r.Choices[0].Message.Content, nil
	}
	return "", fmt.Errorf("empty AI response (status %d)", resp.StatusCode)
}

func postAnthropic(url, key string, payload interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := aiHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	if len(r.Content) > 0 {
		return r.Content[0].Text, nil
	}
	return "", fmt.Errorf("empty Anthropic response (status %d)", resp.StatusCode)
}

func postGoogle(url string, payload interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}
	resp, err := aiHTTPClient.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	if len(r.Candidates) > 0 && len(r.Candidates[0].Content.Parts) > 0 {
		return r.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("empty Google response (status %d)", resp.StatusCode)
}

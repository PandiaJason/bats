package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type Provider interface {
	Query(prompt string) (string, error)
	Name() string
}

type OpenAIProvider struct{ Model string }
func (p *OpenAIProvider) Name() string { return "OpenAI (" + p.Model + ")" }
func (p *OpenAIProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" { return "BATS: Consensus reached for prompt: " + prompt, nil }
	
	url := "https://api.openai.com/v1/chat/completions"
	payload := map[string]interface{}{
		"model": p.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	return postJSON(url, "Bearer "+apiKey, payload)
}

type AnthropicProvider struct{ Model string }
func (p *AnthropicProvider) Name() string { return "Anthropic (" + p.Model + ")" }
func (p *AnthropicProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" { return "BATS: Consensus reached for prompt: " + prompt, nil }

	url := "https://api.anthropic.com/v1/messages"
	payload := map[string]interface{}{
		"model": p.Model,
		"max_tokens": 1024,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	// Note: Anthropic requires x-api-key header and anthropic-version
	return postAnthropic(url, apiKey, payload)
}

type GoogleProvider struct{ Model string }
func (p *GoogleProvider) Name() string { return "Google (" + p.Model + ")" }
func (p *GoogleProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" { return "BATS: Consensus reached for prompt: " + prompt, nil }

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", p.Model, apiKey)
	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": prompt}}},
		},
	}
	return postGoogle(url, payload)
}

func postJSON(url, auth string, payload interface{}) (string, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" { req.Header.Set("Authorization", auth) }
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Choices) > 0 { return r.Choices[0].Message.Content, nil }
	return "", fmt.Errorf("empty AI response")
}

func postAnthropic(url, key string, payload interface{}) (string, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Content) > 0 { return r.Content[0].Text, nil }
	return "", fmt.Errorf("empty Anthropic response")
}

func postGoogle(url string, payload interface{}) (string, error) {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil { return "", err }
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
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Candidates) > 0 && len(r.Candidates[0].Content.Parts) > 0 {
		return r.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("empty Google response")
}

func GetProvider(name string) Provider {
	switch name {
	case "openai": return &OpenAIProvider{Model: "gpt-4o-mini"}
	case "anthropic": return &AnthropicProvider{Model: "claude-3-5-sonnet-20240620"}
	case "google": return &GoogleProvider{Model: "gemini-1.5-pro"}
	default: return &OpenAIProvider{Model: "gpt-4o-mini"}
	}
}

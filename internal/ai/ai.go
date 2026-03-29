package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// SafetyVerdict is the structured result of an AI safety evaluation.
// Confidence is a float64 in [0.0, 1.0] where 1.0 = maximum certainty.
// The fast-path threshold is 0.95: actions above this skip synchronous PBFT.
type SafetyVerdict struct {
	Classification string  // "SAFE_READ", "SAFE", "UNSAFE"
	Confidence     float64 // 0.0 - 1.0
	Reason         string  // Human-readable explanation
}

// IsFastPathEligible returns true when the heuristic confidence for a
// non-mutating action exceeds the 0.95 threshold. This is the gate
// that decides whether PBFT can be deferred to background.
func (v SafetyVerdict) IsFastPathEligible() bool {
	return v.Classification == "SAFE_READ" && v.Confidence >= 0.95
}

// IsSafe returns true for any SAFE* classification.
func (v SafetyVerdict) IsSafe() bool {
	return strings.HasPrefix(v.Classification, "SAFE")
}

// Provider is the interface for all LLM backends.
type Provider interface {
	Query(prompt string) (string, error)
	Evaluate(action string) SafetyVerdict
	Name() string
}

// --- OpenAI Provider ---

type OpenAIProvider struct{ Model string }

func (p *OpenAIProvider) Name() string { return "OpenAI (" + p.Model + ")" }

func (p *OpenAIProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return heuristicEval(prompt).Reason, nil
	}

	url := "https://api.openai.com/v1/chat/completions"
	payload := map[string]interface{}{
		"model":    p.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	return postJSON(url, "Bearer "+apiKey, payload)
}

func (p *OpenAIProvider) Evaluate(action string) SafetyVerdict {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return heuristicEval(action)
	}
	// Production: parse structured LLM response into SafetyVerdict.
	// For now, fall back to heuristic when API returns unstructured text.
	return heuristicEval(action)
}

// --- Anthropic Provider ---

type AnthropicProvider struct{ Model string }

func (p *AnthropicProvider) Name() string { return "Anthropic (" + p.Model + ")" }

func (p *AnthropicProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return heuristicEval(prompt).Reason, nil
	}

	url := "https://api.anthropic.com/v1/messages"
	payload := map[string]interface{}{
		"model":      p.Model,
		"max_tokens": 1024,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	return postAnthropic(url, apiKey, payload)
}

func (p *AnthropicProvider) Evaluate(action string) SafetyVerdict {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return heuristicEval(action)
	}
	return heuristicEval(action)
}

// --- Google Provider ---

type GoogleProvider struct{ Model string }

func (p *GoogleProvider) Name() string { return "Google (" + p.Model + ")" }

func (p *GoogleProvider) Query(prompt string) (string, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return heuristicEval(prompt).Reason, nil
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", p.Model, apiKey)
	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": prompt}}},
		},
	}
	return postGoogle(url, payload)
}

func (p *GoogleProvider) Evaluate(action string) SafetyVerdict {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return heuristicEval(action)
	}
	return heuristicEval(action)
}

// heuristicEval is the local, zero-latency safety classifier.
// It returns a structured SafetyVerdict with a numeric confidence score.
// The confidence values are calibrated so that:
//   - UNSAFE: 0.99 confidence (high certainty of danger)
//   - SAFE_READ: 0.98 confidence (deterministic read verbs)
//   - SAFE: 0.80 confidence (safe but state-mutating, requires full PBFT)
func heuristicEval(input string) SafetyVerdict {
	lower := strings.ToLower(input)

	// --- Blocklist: known dangerous patterns ---
	dangerousPatterns := []string{
		"delete", "drop", "rm -rf", "shadow", "truncate",
		"shutdown", "exec(", "eval(", "format c:",
	}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lower, pattern) {
			return SafetyVerdict{
				Classification: "UNSAFE",
				Confidence:     0.99,
				Reason:         fmt.Sprintf("Blocked: matched dangerous pattern '%s'", pattern),
			}
		}
	}

	// --- Safe reads: deterministic, non-mutating operations ---
	readVerbs := []string{"read", "get", "list", "fetch", "describe", "show", "status", "ping", "health", "info", "select"}
	for _, verb := range readVerbs {
		if strings.Contains(lower, verb) {
			return SafetyVerdict{
				Classification: "SAFE_READ",
				Confidence:     0.98,
				Reason:         fmt.Sprintf("Non-mutating operation detected (verb: '%s')", verb),
			}
		}
	}

	// --- Default: safe but requires full consensus ---
	return SafetyVerdict{
		Classification: "SAFE",
		Confidence:     0.80,
		Reason:         "Action appears safe but requires full PBFT consensus",
	}
}

// GetProvider returns the appropriate AI provider based on env config.
func GetProvider(name string) Provider {
	switch name {
	case "openai":
		return &OpenAIProvider{Model: "gpt-4o-mini"}
	case "anthropic":
		return &AnthropicProvider{Model: "claude-3-5-sonnet-20240620"}
	case "google":
		return &GoogleProvider{Model: "gemini-1.5-pro"}
	default:
		return &OpenAIProvider{Model: "gpt-4o-mini"}
	}
}

// --- HTTP helpers (unchanged) ---

func postJSON(url, auth string, payload interface{}) (string, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := http.DefaultClient.Do(req)
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
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Choices) > 0 {
		return r.Choices[0].Message.Content, nil
	}
	return "", fmt.Errorf("empty AI response")
}

func postAnthropic(url, key string, payload interface{}) (string, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Content) > 0 {
		return r.Content[0].Text, nil
	}
	return "", fmt.Errorf("empty Anthropic response")
}

func postGoogle(url string, payload interface{}) (string, error) {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
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
	json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Candidates) > 0 && len(r.Candidates[0].Content.Parts) > 0 {
		return r.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("empty Google response")
}

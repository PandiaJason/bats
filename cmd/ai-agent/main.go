package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Config struct {
	APIKey string
	Model  string
	Prompt string
	Node   string
}

func main() {
	config := Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
		Model:  "gpt-4o-mini",
		Prompt: "Summarize WAND (Watch. Audit. Never Delegate.) in 1 sentence. Use EXACTLY these words in order: 'Deterministic safety enforcement for agents.'",
		Node:   "localhost:8001",
	}

	if config.APIKey == "" {
		fmt.Println("Warning: OPENAI_API_KEY not set. Running in MOCK mode.")
		mockLLMDecision(config)
		return
	}

	runAIConsensus(config)
}

func runAIConsensus(config Config) {
	fmt.Printf("Querying OpenAI [%s]...\n", config.Model)

	url := "https://api.openai.com/v1/chat/completions"
	payload := map[string]interface{}{
		"model": config.Model,
		"messages": []map[string]string{
			{"role": "user", "content": config.Prompt},
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("API error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var aiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(respBody, &aiResp)

	content := aiResp.Choices[0].Message.Content
	fmt.Printf("AI Result: %s\n", content)

	submitToCluster(config.Node, content)
}

func mockLLMDecision(config Config) {
	content := "Deterministic safety enforcement for agents."
	fmt.Printf("Mock AI Result: %s\n", content)
	submitToCluster(config.Node, content)
}

func submitToCluster(node string, content string) {
	fmt.Printf("Submitting AI Result to node %s for validation...\n", node)

	url := "http://" + node + "/start"
	http.Post(url, "text/plain", bytes.NewBuffer([]byte(content)))
	fmt.Println("✅ AI Decision submitted to WAND cluster for verification.")
}

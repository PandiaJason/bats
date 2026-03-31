package ai

import (
	"testing"
)

func TestHeuristicEval(t *testing.T) {
	tests := []struct {
		input      string
		wantClass  string
		wantConf   float64
	}{
		{"rm -rf /", "UNSAFE", 0.99},
		{"delete the database", "UNSAFE", 0.99},
		{"ls -la", "SAFE_READ", 0.98},
		{"cat main.go", "SAFE_READ", 0.98},
		{"git commit -m 'feat'", "SAFE", 0.80},
	}

	for _, tt := range tests {
		v := heuristicEval(tt.input)
		if v.Classification != tt.wantClass {
			t.Errorf("heuristicEval(%q) classification = %v, want %v", tt.input, v.Classification, tt.wantClass)
		}
		if v.Confidence != tt.wantConf {
			t.Errorf("heuristicEval(%q) confidence = %v, want %v", tt.input, v.Confidence, tt.wantConf)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		input      string
		wantClass  string
		wantConf   float64
	}{
		{
			name:      "Standard JSON (Keyword Override on UNSAFE)",
			json:      `{"classification": "UNSAFE", "confidence": 0.95, "reason": "destructive"}`,
			input:     "wipe the drive",
			wantClass: "UNSAFE",
			wantConf:  0.99, // 'wipe' keyword in input forces 0.99
		},
		{
			name:      "Markdown Wrapped",
			json:      "```json\n{\"classification\": \"SAFE\", \"confidence\": 0.85, \"reason\": \"safe edit\"}\n```",
			input:     "edit index.html",
			wantClass: "SAFE",
			wantConf:  0.85,
		},
		{
			name:      "Keyword Override (Keywords win on UNSAFE)",
			json:      `{"classification": "SAFE", "confidence": 0.90, "reason": "false positive"}`,
			input:     "rm -rf /",
			wantClass: "UNSAFE", // Heuristic floor should catch it
			wantConf:  0.99,
		},
		{
			name:      "Garbage Fallback (Heuristic Safe Read)",
			json:      "not json",
			input:     "ls -la",
			wantClass: "SAFE_READ",
			wantConf:  0.98,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := parseVerdict(tt.json, tt.input)
			if v.Classification != tt.wantClass {
				t.Errorf("%s: classification = %v, want %v", tt.name, v.Classification, tt.wantClass)
			}
			if v.Confidence != tt.wantConf {
				t.Errorf("%s: confidence = %v, want %v", tt.name, v.Confidence, tt.wantConf)
			}
		})
	}
}

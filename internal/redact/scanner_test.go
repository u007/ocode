package redact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLocalEndpoint(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"http://localhost:1234/v1", true},
		{"http://127.0.0.1:11434", true},
		{"http://[::1]:8080", true},
		{"https://api.openai.com/v1", false},
		{"http://192.168.1.100:8080", false},
		{"http://my-lan-server:8080", false},
		{"http://10.0.0.1:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := IsLocalEndpoint(tt.url)
			if got != tt.expected {
				t.Errorf("IsLocalEndpoint(%q) = %v, want %v", tt.url, got, tt.expected)
			}
		})
	}
}

func TestLLMScannerNonLocal(t *testing.T) {
	scanner := &LLMScanner{
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4",
	}

	_, err := scanner.Scan("test text")
	if err == nil {
		t.Error("Expected error for non-local endpoint")
	}
}

func TestLLMScannerLocal(t *testing.T) {
	// Create a fake local server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("Expected /chat/completions, got %s", r.URL.Path)
		}

		// Parse request
		var req scanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Model != "test-model" {
			t.Errorf("Expected model test-model, got %s", req.Model)
		}

		// Return response with a secret
		resp := scanResponse{
			Choices: []scanChoice{
				{
					Message: scanMessage{
						Role:    "assistant",
						Content: `["actual-secret-value"]`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	scanner := &LLMScanner{
		BaseURL: server.URL,
		Model:   "test-model",
	}

	spans, err := scanner.Scan("text with actual-secret-value")
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	if spans[0].Kind != "model" {
		t.Errorf("Expected kind model, got %s", spans[0].Kind)
	}
}

func TestLLMScannerDropsHallucinations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := scanResponse{
			Choices: []scanChoice{
				{
					Message: scanMessage{
						Role:    "assistant",
						Content: `["hallucinated-secret-not-in-text"]`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	scanner := &LLMScanner{
		BaseURL: server.URL,
		Model:   "test-model",
	}

	spans, err := scanner.Scan("text without any secrets")
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if len(spans) != 0 {
		t.Errorf("Expected 0 spans (hallucination dropped), got %d", len(spans))
	}
}

func TestLLMScannerDropsTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := scanResponse{
			Choices: []scanChoice{
				{
					Message: scanMessage{
						Role:    "assistant",
						Content: `["[[OCSEC:a3f9c2:1]]"]`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	scanner := &LLMScanner{
		BaseURL: server.URL,
		Model:   "test-model",
	}

	spans, err := scanner.Scan("text with [[OCSEC:a3f9c2:1]]")
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if len(spans) != 0 {
		t.Errorf("Expected 0 spans (token dropped), got %d", len(spans))
	}
}

func TestParseScannerOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		input    string
		expected []Span
	}{
		{
			name:     "valid JSON",
			output:   `["secret123", "password456"]`,
			input:    "text with secret123 and password456",
			expected: []Span{{Start: 10, End: 19, Kind: "model"}, {Start: 24, End: 35, Kind: "model"}},
		},
		{
			name:     "markdown wrapped",
			output:   "```json\n[\"secret123\"]\n```",
			input:    "text with secret123",
			expected: []Span{{Start: 10, End: 19, Kind: "model"}},
		},
		{
			name:     "empty array",
			output:   `[]`,
			input:    "text without secrets",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans, err := parseScannerOutput(tt.output, tt.input)
			if err != nil {
				t.Fatalf("parseScannerOutput error: %v", err)
			}

			if len(spans) != len(tt.expected) {
				t.Fatalf("Expected %d spans, got %d", len(tt.expected), len(spans))
			}

			for i, span := range spans {
				if span.Start != tt.expected[i].Start || span.End != tt.expected[i].End {
					t.Errorf("Span %d: got [%d:%d], want [%d:%d]",
						i, span.Start, span.End, tt.expected[i].Start, tt.expected[i].End)
				}
			}
		})
	}
}

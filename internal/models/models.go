package models

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/pricing"
)

// requestyBaseURL is the Requesty API base URL for listing models.
const requestyBaseURL = "https://router.requesty.ai/v1"

// requestyProvider is the provider identifier used to trigger Requesty API routing.
const requestyProvider = "requesty"

// requestyModel is a model entry in the Requesty models list API response.
// NOTE: Arch, Format, and Coding are not available from the Requesty API;
// those fields in ModelEntry default to zero values for Requesty models.
type requestyModel struct {
	ID            string  `json:"id"`
	Object        string  `json:"object"`
	Created       int64   `json:"created"`
	OwnedBy       string  `json:"owned_by"`
	InputPrice    float64 `json:"input_price,omitempty"`
	OutputPrice   float64 `json:"output_price,omitempty"`
	ContextWindow int     `json:"context_window,omitempty"`
	SupportsVision bool   `json:"supports_vision,omitempty"`
}

// requestyModelName derives a friendly display name from a Requesty model ID.
// For IDs like "openai/gpt-4o", it returns "gpt-4o". For IDs without a
// slash, it returns the full ID as-is.
func requestyModelName(modelID string) string {
	if idx := strings.Index(modelID, "/"); idx > 0 && idx < len(modelID)-1 {
		return modelID[idx+1:]
	}
	return modelID
}

type ModelEntry struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Provider   string  `json:"provider"`
	Arch       string  `json:"arch"`
	Format     string  `json:"format"`
	Coding     bool    `json:"coding"`
	Vision     bool    `json:"vision"`
	ContextLen int     `json:"context_length"`
	Pricing    Pricing `json:"pricing"`
}

type Pricing struct {
	Prompt  float64
	Comp    float64
	Image   float64
	Request float64
}

func (p *Pricing) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Prompt = parseFloat(raw["prompt"])
	p.Comp = parseFloat(raw["completion"])
	p.Image = parseFloat(raw["image"])
	p.Request = parseFloat(raw["request"])
	return nil
}

func parseFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	if s, ok := v.(string); ok {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			log.Printf("warn: models: failed to parse price %q: %v", s, err)
			return 0
		}
		return f
	}
	return 0
}

func FetchAll() ([]ModelEntry, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var openRouterResp struct {
		Data []ModelEntry `json:"data"`
	}
	if err := json.Unmarshal(body, &openRouterResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	entries := openRouterResp.Data
	for i, m := range entries {
		if p, ok := pricing.Lookup(m.ID); ok {
			entries[i].Pricing.Prompt = p.InputPerMillion / 1_000_000
			entries[i].Pricing.Comp = p.OutputPerMillion / 1_000_000
		}
	}
	return entries, nil
}

// FetchRequestyModels fetches available models from the Requesty API.
// If apiKey is non-empty, only models approved for the organization are returned;
// otherwise all public models are returned.
func FetchRequestyModels(apiKey string) ([]ModelEntry, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", requestyBaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("fetch requesty models: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch requesty models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch requesty models: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read requesty response: %w", err)
	}

	var requestyResp struct {
		Object string          `json:"object"`
		Data   []requestyModel `json:"data"`
	}
	if err := json.Unmarshal(body, &requestyResp); err != nil {
		return nil, fmt.Errorf("parse requesty response: %w", err)
	}

	entries := make([]ModelEntry, 0, len(requestyResp.Data))
	for _, rm := range requestyResp.Data {
		// Parse the provider from the model ID (e.g. "openai/gpt-4o" -> "openai")
		provider := ""
		if idx := strings.Index(rm.ID, "/"); idx > 0 {
			provider = rm.ID[:idx]
		}

		entry := ModelEntry{
			ID:         rm.ID,
			Name:       requestyModelName(rm.ID),
			Provider:   provider,
			ContextLen: rm.ContextWindow,
			Pricing: Pricing{
				Prompt: rm.InputPrice,
				Comp:   rm.OutputPrice,
			},
			Vision: rm.SupportsVision,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// fetchModels fetches models from the appropriate API based on provider.
// When provider is requestyProvider, it fetches from Requesty's API using the
// REQUESTY_API_KEY env var. Otherwise it fetches from OpenRouter.
// The caller is responsible for applying filterByProvider when needed.
func fetchModels(provider string) ([]ModelEntry, error) {
	if provider == requestyProvider {
		apiKey := os.Getenv("REQUESTY_API_KEY")
		return FetchRequestyModels(apiKey)
	}
	return FetchAll()
}

// filterByProvider applies provider-based filtering to the model list.
// For Requesty models, filtering is skipped because the provider argument
// selects the API source (requestyProvider routes to Requesty's API) rather than
// filtering results. For other providers, it filters models whose ID
// contains the provider string (case-insensitive).
func filterByProvider(models []ModelEntry, provider string) []ModelEntry {
	if provider == requestyProvider {
		return models
	}
	return filterModels(models, provider)
}

func filterModels(models []ModelEntry, provider string) []ModelEntry {
	if provider == "" {
		return models
	}
	var filtered []ModelEntry
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.ID), strings.ToLower(provider)) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func List(provider string) error {
	models, err := fetchModels(provider)
	if err != nil {
		return err
	}

	models = filterByProvider(models, provider)

	if len(models) == 0 {
		fmt.Println("No models found.")
		return nil
	}

	fmt.Printf("%-50s %-35s %10s %14s %14s\n", "ID", "Name", "Context", "Prompt ($/M)", "Comp ($/M)")
	fmt.Println(strings.Repeat("-", 125))
	for _, m := range models {
		fmt.Printf("%-50s %-35s %10d %14.6f %14.6f\n",
			truncate(m.ID, 50),
			truncate(m.Name, 35),
			m.ContextLen,
			m.Pricing.Prompt*1_000_000,
			m.Pricing.Comp*1_000_000,
		)
	}
	fmt.Printf("\nTotal: %d models\n", len(models))
	return nil
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}

func Run(args []string) error {
	// Check for help flag
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printModelsUsage()
			return nil
		}
	}

	provider := ""
	for i, a := range args {
		switch a {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
			}
		default:
			if !strings.HasPrefix(a, "-") {
				provider = a
			}
		}
	}

	if os.Getenv("NO_COLOR") == "" {
		return List(provider)
	}

	// NO_COLOR mode: output bare model IDs (one per line).
	models, err := fetchModels(provider)
	if err != nil {
		return err
	}

	models = filterByProvider(models, provider)
	for _, m := range models {
		fmt.Println(m.ID)
	}
	return nil
}

func printModelsUsage() {
	fmt.Println("Usage: ocode models [options] [provider]")
	fmt.Println()
	fmt.Println("List available models. By default fetches from OpenRouter. Use")
	fmt.Println("\"requesty\" to list models from Requesty's API.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  <provider>    Filter models by provider; use \"requesty\" to list")
	fmt.Println("                models from Requesty's API")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -p, --provider <provider>    Filter models by provider")
	fmt.Println("  -h, --help                   Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ocode models")
	fmt.Println("  ocode models openai")
	fmt.Println("  ocode models --provider anthropic")
	fmt.Println("  ocode models requesty")
	fmt.Println("  ocode models --provider requesty")
}

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

	"github.com/jamesmercstudio/ocode/internal/pricing"
)

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
	models, err := FetchAll()
	if err != nil {
		return err
	}

	models = filterModels(models, provider)

	if len(models) == 0 {
		fmt.Println("No models found.")
		return nil
	}

	fmt.Printf("%-45s %-35s %10s %14s %14s\n", "ID", "Name", "Context", "Prompt ($/M)", "Comp ($/M)")
	fmt.Println(strings.Repeat("-", 115))
	for _, m := range models {
		fmt.Printf("%-45s %-35s %10d %14.4f %14.4f\n",
			truncate(m.ID, 45),
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

	models, err := FetchAll()
	if err != nil {
		return err
	}

	models = filterModels(models, provider)

	for _, m := range models {
		fmt.Println(m.ID)
	}
	return nil
}

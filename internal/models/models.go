package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type ModelEntry struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	Arch       string   `json:"arch"`
	Format     string   `json:"format"`
	Coding     bool     `json:"coding"`
	Vision     bool     `json:"vision"`
	ContextLen int      `json:"context_length"`
	Pricing    Pricing  `json:"pricing"`
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
		f, _ := strconv.ParseFloat(s, 64)
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

	return openRouterResp.Data, nil
}

func List(provider string) error {
	models, err := FetchAll()
	if err != nil {
		return err
	}

	if provider != "" {
		var filtered []ModelEntry
		for _, m := range models {
			if strings.Contains(strings.ToLower(m.ID), strings.ToLower(provider)) {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}

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
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
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

	if provider != "" {
		var filtered []ModelEntry
		for _, m := range models {
			if strings.Contains(strings.ToLower(m.ID), strings.ToLower(provider)) {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}

	for _, m := range models {
		fmt.Println(m.ID)
	}
	return nil
}

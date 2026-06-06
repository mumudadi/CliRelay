package providers

import (
	"errors"
	"reflect"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestReplaceOpenAICompatibilityNormalizesFiltersAndRollsBack(t *testing.T) {
	validationErr := errors.New("duplicate channel")
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "existing",
			BaseURL: "https://old.example",
		}},
	}

	err := NewService(cfg, func() error { return validationErr }).ReplaceOpenAICompatibility([]config.OpenAICompatibility{{
		Name:    " next ",
		BaseURL: " https://next.example ",
	}})
	if !errors.Is(err, validationErr) {
		t.Fatalf("ReplaceOpenAICompatibility() error = %v, want validation error", err)
	}
	if got := cfg.OpenAICompatibility; len(got) != 1 || got[0].Name != "existing" {
		t.Fatalf("OpenAICompatibility after rollback = %#v, want existing entry", got)
	}

	err = NewService(cfg, nil).ReplaceOpenAICompatibility([]config.OpenAICompatibility{
		{
			Name:    " next ",
			BaseURL: " https://next.example ",
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{{
				APIKey:   " key-1 ",
				ProxyURL: " http://proxy.example ",
				ProxyID:  " proxy-a ",
			}},
			Headers: map[string]string{" x-trace ": " on "},
		},
		{
			Name:    "blank",
			BaseURL: " ",
		},
	})
	if err != nil {
		t.Fatalf("ReplaceOpenAICompatibility() error = %v, want nil", err)
	}
	got := cfg.OpenAICompatibility
	if len(got) != 1 {
		t.Fatalf("OpenAICompatibility len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Name != "next" || got[0].BaseURL != "https://next.example" {
		t.Fatalf("normalized entry = %#v, want trimmed values", got[0])
	}
	if got[0].APIKeyEntries[0].APIKey != "key-1" || got[0].APIKeyEntries[0].ProxyURL != "http://proxy.example" || got[0].APIKeyEntries[0].ProxyID != "proxy-a" {
		t.Fatalf("normalized api key entry = %#v", got[0].APIKeyEntries[0])
	}
	if _, ok := got[0].Headers["x-trace"]; !ok {
		t.Fatalf("headers = %#v, want normalized x-trace header", got[0].Headers)
	}
}

func TestPatchOpenAICompatibilityUpdatesAndDeletes(t *testing.T) {
	cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
		Name:    "compat",
		BaseURL: "https://old.example",
	}}}
	name := " compat "
	newName := " renamed "
	disabled := true
	baseURL := " https://new.example "
	models := []config.OpenAICompatibilityModel{{Name: " gpt-4.1 ", Alias: " smart "}}

	err := NewService(cfg, nil).PatchOpenAICompatibility(nil, &name, OpenAICompatibilityPatch{
		Name:     &newName,
		Disabled: &disabled,
		BaseURL:  &baseURL,
		Models:   &models,
	})
	if err != nil {
		t.Fatalf("PatchOpenAICompatibility() error = %v, want nil", err)
	}
	if got := cfg.OpenAICompatibility[0]; got.Name != "renamed" || got.BaseURL != "https://new.example" || !got.Disabled {
		t.Fatalf("patched entry = %#v, want trimmed updated entry", got)
	}

	index := 0
	emptyBaseURL := " "
	err = NewService(cfg, nil).PatchOpenAICompatibility(&index, nil, OpenAICompatibilityPatch{BaseURL: &emptyBaseURL})
	if err != nil {
		t.Fatalf("PatchOpenAICompatibility(delete) error = %v, want nil", err)
	}
	if len(cfg.OpenAICompatibility) != 0 {
		t.Fatalf("OpenAICompatibility after delete = %#v, want empty", cfg.OpenAICompatibility)
	}
}

func TestVertexCompatKeysNormalizePatchAndDelete(t *testing.T) {
	cfg := &config.Config{}
	svc := NewService(cfg, nil)

	svc.ReplaceVertexCompatKeys([]config.VertexCompatKey{{
		APIKey:  " vertex-key ",
		BaseURL: " https://vertex.example ",
		Headers: map[string]string{
			" x-trace ": " on ",
		},
		Models: []config.VertexCompatModel{
			{Name: " gemini-pro ", Alias: " pro "},
			{Name: " ", Alias: "drop"},
		},
	}})

	if len(cfg.VertexCompatAPIKey) != 1 {
		t.Fatalf("VertexCompatAPIKey len = %d, want 1", len(cfg.VertexCompatAPIKey))
	}
	got := cfg.VertexCompatAPIKey[0]
	if got.APIKey != "vertex-key" || got.BaseURL != "https://vertex.example" {
		t.Fatalf("normalized vertex entry = %#v", got)
	}
	if !reflect.DeepEqual(got.Models, []config.VertexCompatModel{{Name: "gemini-pro", Alias: "pro"}}) {
		t.Fatalf("normalized models = %#v, want one trimmed model", got.Models)
	}

	match := " vertex-key "
	proxyURL := " http://proxy.example "
	err := svc.PatchVertexCompatKey(nil, &match, VertexCompatPatch{ProxyURL: &proxyURL})
	if err != nil {
		t.Fatalf("PatchVertexCompatKey() error = %v, want nil", err)
	}
	if cfg.VertexCompatAPIKey[0].ProxyURL != "http://proxy.example" {
		t.Fatalf("ProxyURL = %q, want trimmed proxy URL", cfg.VertexCompatAPIKey[0].ProxyURL)
	}

	index := 0
	emptyAPIKey := " "
	err = svc.PatchVertexCompatKey(&index, nil, VertexCompatPatch{APIKey: &emptyAPIKey})
	if err != nil {
		t.Fatalf("PatchVertexCompatKey(delete) error = %v, want nil", err)
	}
	if len(cfg.VertexCompatAPIKey) != 0 {
		t.Fatalf("VertexCompatAPIKey after delete = %#v, want empty", cfg.VertexCompatAPIKey)
	}
}

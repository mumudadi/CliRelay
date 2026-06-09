package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestGetPublicUsageByAPIKeyMasksCredentialEverywhere(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const apiKey = "sk-test-public-usage-secret-123456"
	stats := usage.NewRequestStatistics()
	wasEnabled := usage.StatisticsEnabled()
	usage.SetStatisticsEnabled(true)
	t.Cleanup(func() { usage.SetStatisticsEnabled(wasEnabled) })
	stats.Record(context.Background(), coreusage.Record{
		APIKey: apiKey,
		Model:  "gpt-4o-mini",
		Detail: coreusage.Detail{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
	})

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/public/usage", bytes.NewReader([]byte(`{"api_key":"`+apiKey+`"}`)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h := NewHandler(&config.Config{}, "", nil)
	h.SetUsageStatistics(stats)
	h.GetPublicUsageByAPIKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), apiKey) {
		t.Fatalf("public response leaked full API key: %s", rec.Body.String())
	}

	var got struct {
		APIKey string `json:"api_key"`
		Usage  struct {
			APIs map[string]usage.APISnapshot `json:"apis"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.APIKey == apiKey || got.APIKey == "" {
		t.Fatalf("api_key = %q, want masked non-empty value", got.APIKey)
	}
	if _, exists := got.Usage.APIs[apiKey]; exists {
		t.Fatalf("usage.apis contains raw API key")
	}
	if _, exists := got.Usage.APIs[got.APIKey]; !exists {
		t.Fatalf("usage.apis does not contain masked key %q: %#v", got.APIKey, got.Usage.APIs)
	}
}

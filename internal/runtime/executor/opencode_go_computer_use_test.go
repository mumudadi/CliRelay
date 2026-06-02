package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ---------------------------------------------------------------------------
// Unit: opencodeGoInjectComputerUseTools
// ---------------------------------------------------------------------------

func TestInjectComputerUseTools_AddsWhenMissing(t *testing.T) {
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"existing_tool"}}]}`)
	got := opencodeGoInjectComputerUseTools(payload)

	tools := gjson.GetBytes(got, "tools").Array()
	foundExisting := false
	foundComputerUse := false
	for _, tool := range tools {
		name := tool.Get("function.name").String()
		if name == "existing_tool" {
			foundExisting = true
		}
		if name == "mcp__computer_use__click" {
			foundComputerUse = true
		}
	}
	if !foundExisting {
		t.Error("original tool 'existing_tool' was removed")
	}
	if !foundComputerUse {
		t.Error("mcp__computer_use__click was not injected")
	}
	if len(tools) != 11 {
		t.Errorf("expected 11 tools (1 existing + 10 CU), got %d", len(tools))
	}
}

func TestInjectComputerUseTools_SkipsWhenAlreadyPresent(t *testing.T) {
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"mcp__computer_use__click"}}]}`)
	got := opencodeGoInjectComputerUseTools(payload)

	tools := gjson.GetBytes(got, "tools").Array()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (skip injection since CU already present), got %d", len(tools))
	}
}

func TestInjectComputerUseTools_SkipsNoTools(t *testing.T) {
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`)
	got := opencodeGoInjectComputerUseTools(payload)

	if gjson.GetBytes(got, "tools").Exists() {
		t.Error("should not add tools array when none existed")
	}
}

func TestInjectComputerUseTools_EmptyTools(t *testing.T) {
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"tools":[]}`)
	got := opencodeGoInjectComputerUseTools(payload)

	tools := gjson.GetBytes(got, "tools").Array()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for empty array, got %d", len(tools))
	}
}

func TestInjectComputerUseTools_InvalidJSON(t *testing.T) {
	got := opencodeGoInjectComputerUseTools([]byte(`not json`))
	if string(got) != "not json" {
		t.Error("should return original payload unchanged for invalid JSON")
	}
}

func TestInjectComputerUseTools_NilPayload(t *testing.T) {
	got := opencodeGoInjectComputerUseTools(nil)
	if got != nil {
		t.Error("should return nil for nil payload")
	}
}

func TestInjectComputerUseTools_SkipsNamespaceAlreadyPresent(t *testing.T) {
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"namespace","name":"mcp__computer_use__"}]}`)
	got := opencodeGoInjectComputerUseTools(payload)

	tools := gjson.GetBytes(got, "tools").Array()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (skip injection due to existing namespace), got %d", len(tools))
	}
}

func TestInjectComputerUseTools_PreservesModelAndMessages(t *testing.T) {
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"test"}],"tools":[{"type":"function","function":{"name":"foo"}}]}`)
	got := opencodeGoInjectComputerUseTools(payload)

	if model := gjson.GetBytes(got, "model").String(); model != "deepseek-v4-flash" {
		t.Errorf("model changed, got %q", model)
	}
	if msg := gjson.GetBytes(got, "messages.0.content").String(); msg != "test" {
		t.Errorf("messages changed, got %q", msg)
	}
}

// ---------------------------------------------------------------------------
// Verify all 10 Computer Use functions are well-formed
// ---------------------------------------------------------------------------

func TestComputerUseFunctions_AllValidDefinitions(t *testing.T) {
	for i, fn := range mcpComputerUseFunctions {
		fnMap, ok := fn["function"].(map[string]any)
		if !ok {
			t.Fatalf("function %d missing 'function' key", i)
		}
		name, _ := fnMap["name"].(string)
		if !hasPrefix(name, "mcp__computer_use__") {
			t.Errorf("function %d has unexpected name %q", i, name)
		}
		// Verify it can be serialized
		_, err := json.Marshal(fn)
		if err != nil {
			t.Fatalf("function %d (%s) failed to marshal: %v", i, name, err)
		}
		// Verify sjson.SetBytes works with it
		testPayload := []byte(`{"tools":[]}`)
		_, err = sjson.SetBytes(testPayload, "tools.0", fn)
		if err != nil {
			t.Fatalf("function %d (%s) failed sjson roundtrip: %v", i, name, err)
		}
	}
}

func TestComputerUseFunctions_Count(t *testing.T) {
	if len(mcpComputerUseFunctions) != 10 {
		t.Fatalf("expected 10 Computer Use functions, got %d", len(mcpComputerUseFunctions))
	}

	expectedNames := []string{
		"mcp__computer_use__click",
		"mcp__computer_use__drag",
		"mcp__computer_use__get_app_state",
		"mcp__computer_use__list_apps",
		"mcp__computer_use__perform_secondary_action",
		"mcp__computer_use__press_key",
		"mcp__computer_use__scroll",
		"mcp__computer_use__select_text",
		"mcp__computer_use__set_value",
		"mcp__computer_use__type_text",
	}
	for _, expected := range expectedNames {
		found := false
		for _, fn := range mcpComputerUseFunctions {
			fnMap, _ := fn["function"].(map[string]any)
			if fnMap["name"].(string) == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected function %q not found", expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: Execute with Computer Use injection (DeepSeek models)
// ---------------------------------------------------------------------------

func TestOpenCodeGoExecutorInjectsComputerUseTools(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_cu","object":"chat.completion","created":1,"model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer server.Close()

	oldURL := opencodeGoBaseURL
	opencodeGoBaseURL = server.URL + "/v1"
	t.Cleanup(func() { opencodeGoBaseURL = oldURL })

	exec := NewOpenCodeGoExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key"}}
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"use computer"}],"tools":[{"type":"function","function":{"name":"existing_tool"}}]}`)

	_, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-v4-flash",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Verify upstream body has Computer Use tools injected
	tools := gjson.GetBytes(gotBody, "tools").Array()
	hasCU := false
	hasExisting := false
	for _, tool := range tools {
		name := tool.Get("function.name").String()
		if name == "mcp__computer_use__get_app_state" {
			hasCU = true
		}
		if name == "existing_tool" {
			hasExisting = true
		}
	}
	if !hasExisting {
		t.Error("upstream body missing existing_tool")
	}
	if !hasCU {
		t.Errorf("upstream body missing mcp__computer_use__ tools; body=%s", string(gotBody))
	}
}

func TestOpenCodeGoExecutorDoesNotInjectForNonDeepSeek(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_no","object":"chat.completion","created":1,"model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer server.Close()

	oldURL := opencodeGoBaseURL
	opencodeGoBaseURL = server.URL + "/v1"
	t.Cleanup(func() { opencodeGoBaseURL = oldURL })

	exec := NewOpenCodeGoExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key"}}
	payload := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"existing_tool"}}]}`)

	_, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5.5",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Non-deepseek model should NOT get CU tools
	tools := gjson.GetBytes(gotBody, "tools").Array()
	for _, tool := range tools {
		name := tool.Get("function.name").String()
		if hasPrefix(name, "mcp__computer_use__") {
			t.Errorf("non-deepseek model should not get CU tools; found %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: ExecuteStream with Computer Use injection
// ---------------------------------------------------------------------------

func TestOpenCodeGoExecutorStreamInjectsComputerUseTools(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"chunk1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	oldURL := opencodeGoBaseURL
	opencodeGoBaseURL = server.URL + "/v1"
	t.Cleanup(func() { opencodeGoBaseURL = oldURL })

	exec := NewOpenCodeGoExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key"}}
	payload := []byte(`{"model":"deepseek-v4-flash","stream":true,"messages":[{"role":"user","content":"use computer"}],"tools":[{"type":"function","function":{"name":"tool1"}}]}`)

	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-v4-flash",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
	}

	// Verify upstream body has Computer Use tools
	hasCU := false
	for _, tool := range gjson.GetBytes(gotBody, "tools").Array() {
		if hasPrefix(tool.Get("function.name").String(), "mcp__computer_use__") {
			hasCU = true
			break
		}
	}
	if !hasCU {
		t.Errorf("stream request missing mcp__computer_use__ tools; body=%s", string(gotBody))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

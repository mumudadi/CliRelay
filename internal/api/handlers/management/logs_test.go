package management

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestGetLogsReturnsEmptySnapshotWhenFileLoggingDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandler(&config.Config{LoggingToFile: false}, "", nil)
	defer h.Close()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/logs?limit=50000&after=1714567890", nil)

	h.GetLogs(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Lines           []string `json:"lines"`
		LineCount       int      `json:"line-count"`
		LatestTimestamp int64    `json:"latest-timestamp"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Lines) != 0 {
		t.Fatalf("lines = %v, want empty", payload.Lines)
	}
	if payload.LineCount != 0 {
		t.Fatalf("line-count = %d, want 0", payload.LineCount)
	}
	if payload.LatestTimestamp != 1714567890 {
		t.Fatalf("latest-timestamp = %d, want 1714567890", payload.LatestTimestamp)
	}
}

func TestParseLimitRejectsOversizedLimit(t *testing.T) {
	if _, err := parseLimit("20001"); err == nil {
		t.Fatal("expected oversized limit to be rejected")
	}
}

func TestLogAccumulatorReadsGzipRotatedLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main-2026-06-09T12-00-00.log.gz")

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("[2026-06-09 12:00:01] [info ] rotated line\n")); err != nil {
		t.Fatalf("write gzip payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write gzip log file: %v", err)
	}

	acc := newLogAccumulator(0, 10)
	if err := acc.consumeFile(path); err != nil {
		t.Fatalf("consume gzip log: %v", err)
	}
	lines, total, latest := acc.result()
	if total != 1 || len(lines) != 1 || lines[0] != "[2026-06-09 12:00:01] [info ] rotated line" {
		t.Fatalf("unexpected result: total=%d latest=%d lines=%#v", total, latest, lines)
	}
	if latest == 0 {
		t.Fatal("expected latest timestamp to be parsed")
	}
}

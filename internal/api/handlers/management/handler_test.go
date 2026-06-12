package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestHandlerCloseIsIdempotent(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(nil, nil)
	h.Close()
	h.Close()
}

func TestMiddlewareAllowsValidKeyAfterRemoteIPIsBanned(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const managementKey = "correct-management-key"
	hashed, err := bcrypt.GenerateFromPassword([]byte(managementKey), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash test management key: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{
		RemoteManagement: config.RemoteManagement{
			AllowRemote: true,
			SecretKey:   string(hashed),
		},
	}, nil)
	defer h.Close()

	router := gin.New()
	router.Use(h.Middleware())
	router.GET("/v0/management/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
		req.RemoteAddr = "203.0.113.10:4321"
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("missing-key attempt %d status = %d, want %d; body=%s", i+1, rr.Code, http.StatusUnauthorized, rr.Body.String())
		}
	}

	rrBanned := httptest.NewRecorder()
	reqBanned := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
	reqBanned.RemoteAddr = "203.0.113.10:4321"
	router.ServeHTTP(rrBanned, reqBanned)
	if rrBanned.Code != http.StatusForbidden {
		t.Fatalf("banned missing-key status = %d, want %d; body=%s", rrBanned.Code, http.StatusForbidden, rrBanned.Body.String())
	}
	if !strings.Contains(rrBanned.Body.String(), "IP banned") {
		t.Fatalf("expected IP banned response, got %s", rrBanned.Body.String())
	}

	rrValid := httptest.NewRecorder()
	reqValid := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
	reqValid.RemoteAddr = "203.0.113.10:4321"
	reqValid.Header.Set("Authorization", "Bearer "+managementKey)
	router.ServeHTTP(rrValid, reqValid)
	if rrValid.Code != http.StatusOK {
		t.Fatalf("valid-key status after ban = %d, want %d; body=%s", rrValid.Code, http.StatusOK, rrValid.Body.String())
	}

	rrAfterClear := httptest.NewRecorder()
	reqAfterClear := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
	reqAfterClear.RemoteAddr = "203.0.113.10:4321"
	router.ServeHTTP(rrAfterClear, reqAfterClear)
	if rrAfterClear.Code != http.StatusUnauthorized {
		t.Fatalf("missing-key status after valid key cleared ban = %d, want %d; body=%s", rrAfterClear.Code, http.StatusUnauthorized, rrAfterClear.Body.String())
	}
}

func TestMiddlewareAllowsLocalPasswordWithoutRemoteSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.SetLocalPassword("local-management-password")
	defer h.Close()

	router := gin.New()
	router.Use(h.Middleware())
	router.GET("/v0/management/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Header.Set("Authorization", "Bearer local-management-password")
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("local-password status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestMiddlewareRejectsQueryTokenOnNormalHTTPRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const managementKey = "correct-management-key"
	hashed, err := bcrypt.GenerateFromPassword([]byte(managementKey), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash test management key: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{
		RemoteManagement: config.RemoteManagement{
			AllowRemote: true,
			SecretKey:   string(hashed),
		},
	}, nil)
	defer h.Close()

	router := gin.New()
	router.Use(h.Middleware())
	router.GET("/v0/management/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/ping?token="+managementKey, nil)
	req.RemoteAddr = "203.0.113.20:4321"
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusUnauthorized, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "missing management key") {
		t.Fatalf("expected query token to be ignored on normal route, got body=%s", rr.Body.String())
	}
}

func TestMiddlewareAllowsQueryTokenOnlyForSystemStatsWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const managementKey = "correct-management-key"
	hashed, err := bcrypt.GenerateFromPassword([]byte(managementKey), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash test management key: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{
		RemoteManagement: config.RemoteManagement{
			AllowRemote: true,
			SecretKey:   string(hashed),
		},
	}, nil)
	defer h.Close()

	router := gin.New()
	router.Use(h.Middleware())
	router.GET("/v0/management/system-stats/ws", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/system-stats/ws?token="+managementKey, nil)
	req.RemoteAddr = "203.0.113.21:4321"
	req.Header.Set("Upgrade", "websocket")
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

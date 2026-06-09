package management

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func retryAfterSecondsHeader(duration time.Duration) string {
	seconds := int(duration / time.Second)
	if duration%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

func shouldReadManagementTokenFromQuery(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	if strings.TrimSpace(c.Query("token")) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(c.GetHeader("Upgrade")), "websocket") {
		return false
	}
	path := strings.TrimSpace(c.FullPath())
	if path == "" {
		path = c.Request.URL.Path
	}
	// Query-string credentials are kept only for browser WebSocket handshakes,
	// where custom Authorization headers cannot be set.
	return path == "/v0/management/system-stats/ws" || strings.HasSuffix(path, "/system-stats/ws")
}

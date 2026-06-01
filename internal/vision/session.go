package vision

import (
	"strings"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// ResolveSessionKey extracts a session-level identifier from executor options.
// Returns false when no real session key is available — in that case the caller
// MUST NOT perform cross-turn image memory (single-request only).
//
// Priority:
//  1. opts.Metadata[ExecutionSessionMetadataKey]
//  2. Session-Id header
//  3. Return (empty, false) — never fallback to auth.ID
func ResolveSessionKey(opts cliproxyexecutor.Options, auth *cliproxyauth.Auth) (SessionKey, bool) {
	// 1. Metadata first — most reliable when available
	if raw, ok := opts.Metadata[cliproxyexecutor.ExecutionSessionMetadataKey]; ok {
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			return SessionKey(strings.TrimSpace(s)), true
		}
	}

	// 2. Session-Id header
	if opts.Headers != nil {
		if sessionID := strings.TrimSpace(opts.Headers.Get("Session-Id")); sessionID != "" {
			return SessionKey(sessionID), true
		}
	}

	// 3. Never fallback to auth.ID for image memory — would leak across sessions
	return "", false
}

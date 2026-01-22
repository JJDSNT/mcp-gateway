package transport_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcp-router/internal/config"
	"mcp-router/internal/core"
	"mcp-router/internal/transport"
)

func newTestMux(t *testing.T) *http.ServeMux {
	t.Helper()

	cfg := &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
		Tools: map[string]config.Tool{
			// tool válido (allowlist), mas não queremos chegar a executar
			"echo": {Runtime: "native", Mode: "launcher", Cmd: "true"},
		},
	}

	svc := core.New(cfg)
	httpT := transport.NewHTTP(svc)

	mux := http.NewServeMux()
	httpT.Register(mux)
	return mux
}

func TestHTTPMethods_Hardening(t *testing.T) {
	mux := newTestMux(t)

	tests := []struct {
		method       string
		expectStatus int
	}{
		{http.MethodPost, 0}, // permitido: só verificar != 405
		{http.MethodGet, http.StatusMethodNotAllowed}, // no handler novo: /mcp/<tool> é POST-only

		{http.MethodPut, http.StatusMethodNotAllowed},
		{http.MethodDelete, http.StatusMethodNotAllowed},
		{http.MethodPatch, http.StatusMethodNotAllowed},
		{http.MethodTrace, http.StatusMethodNotAllowed},
		{http.MethodOptions, http.StatusMethodNotAllowed},
		{http.MethodConnect, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/mcp/echo", strings.NewReader("not-json"))
			if tt.method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if tt.expectStatus == 0 {
				if w.Code == http.StatusMethodNotAllowed {
					t.Fatalf("method %s should be allowed (not 405), got %d", tt.method, w.Code)
				}
				return
			}
			if w.Code != tt.expectStatus {
				t.Fatalf("method %s: expected %d, got %d", tt.method, tt.expectStatus, w.Code)
			}
		})
	}
}

func TestContentType_Hardening(t *testing.T) {
	mux := newTestMux(t)

	tests := []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		// aceitos pelo CT, mas body é inválido => 400
		{"json", "application/json", http.StatusBadRequest},
		{"json charset", "application/json; charset=utf-8", http.StatusBadRequest},

		// rejeitados antes de ler/validar JSON
		{"missing", "", http.StatusUnsupportedMediaType},
		{"text", "text/plain", http.StatusUnsupportedMediaType},
		{"form", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"xml", "application/xml", http.StatusUnsupportedMediaType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp/echo", strings.NewReader("not-json"))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("content-type %q: expected %d, got %d", tt.contentType, tt.wantStatus, w.Code)
			}
		})
	}
}

func TestInvalidToolName_Hardening(t *testing.T) {
	mux := newTestMux(t)

	// toolName inválido: tenta traversal / chars proibidos (depende do seu ValidateToolName)
	req := httptest.NewRequest(http.MethodPost, "/mcp/../evil", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid tool name, got %d", w.Code)
	}
}

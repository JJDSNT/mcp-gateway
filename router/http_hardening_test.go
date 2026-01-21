package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcp-router/internal/config"
)

// helper: prepara cfg mínima para o handler real
func setTestConfig() {
	cfg = &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
		Tools: map[string]config.Tool{
			// colocamos um tool válido só para passar da validação de nome e allowlist
			// (mas nos testes abaixo evitamos chegar na execução)
			"echo": {},
		},
	}
}

func TestHTTPMethods_Hardening(t *testing.T) {
	setTestConfig()

	// A ideia é:
	// - métodos não permitidos => 405
	// - GET/POST são permitidos (ou seja, NÃO devem retornar 405).
	tests := []struct {
		method       string
		expectStatus int
	}{
		{http.MethodGet, 0},  // "permitido": só verifica != 405
		{http.MethodPost, 0}, // "permitido": só verifica != 405

		{http.MethodPut, http.StatusMethodNotAllowed},
		{http.MethodDelete, http.StatusMethodNotAllowed},
		{http.MethodPatch, http.StatusMethodNotAllowed},
		{http.MethodTrace, http.StatusMethodNotAllowed},
		{http.MethodOptions, http.StatusMethodNotAllowed},
		{http.MethodConnect, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			// body inválido de propósito para NÃO chegar no runner
			req := httptest.NewRequest(tt.method, "/mcp/echo", strings.NewReader("not-json"))
			w := httptest.NewRecorder()

			handleMCP(w, req)

			if tt.expectStatus == 0 {
				// permitido => não deve ser 405
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
	setTestConfig()

	tests := []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		// aceitos
		{"json", "application/json", http.StatusBadRequest},                 // body inválido => 400 (mas CT ok)
		{"json charset", "application/json; charset=utf-8", http.StatusBadRequest},

		// rejeitados
		{"missing", "", http.StatusUnsupportedMediaType},
		{"text", "text/plain", http.StatusUnsupportedMediaType},
		{"form", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"xml", "application/xml", http.StatusUnsupportedMediaType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// POST com body inválido => queremos testar que o handler rejeita pelo Content-Type antes de qualquer coisa
			req := httptest.NewRequest(http.MethodPost, "/mcp/echo", strings.NewReader("not-json"))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			w := httptest.NewRecorder()

			handleMCP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("content-type %q: expected %d, got %d", tt.contentType, tt.wantStatus, w.Code)
			}
		})
	}
}

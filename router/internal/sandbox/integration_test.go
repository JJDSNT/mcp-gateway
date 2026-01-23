package sandbox

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestToolNameValidation_InRoute verifica que a rota rejeita tool names inválidos
func TestToolNameValidation_InRoute(t *testing.T) {
	tests := map[string]int{
		"valid_tool":      http.StatusNotFound,   // 404 porque tool não existe na config
		"tool/name":       http.StatusBadRequest, // inválido
		"../tool":         http.StatusBadRequest, // inválido
		"tool%2fname":     http.StatusBadRequest, // inválido (encoded /)
		"tool%252fname":   http.StatusBadRequest, // inválido (double-encoded)
		"tool with space": http.StatusBadRequest, // inválido
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extrair tool name (como em main.go)
		toolName := strings.TrimPrefix(r.URL.Path, "/mcp/")
		toolName = strings.Trim(toolName, "/")

		// Validar
		if err := ValidateToolName(toolName); err != nil {
			http.Error(w, "invalid tool name", http.StatusBadRequest)
			return
		}

		// Se válido mas não existe, retornar 404
		http.Error(w, "unknown tool", http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	for path, expectStatus := range tests {
		t.Run(path, func(t *testing.T) {
			url := server.URL + "/mcp/" + path
			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != expectStatus {
				t.Errorf("expected status %d, got %d", expectStatus, resp.StatusCode)
			}
		})
	}
}

// TestAllowlistStrict_OnlyConfiguredTools verifica que apenas tools
// definidas no YAML podem ser executadas
func TestAllowlistStrict_OnlyConfiguredTools(t *testing.T) {
	// Simular config com apenas ferramentas específicas
	allowedTools := map[string]bool{
		"echo":       true,
		"filesystem": true,
		"git":        true,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		toolName := strings.TrimPrefix(r.URL.Path, "/mcp/")
		toolName = strings.Trim(toolName, "/")

		// Validar nome
		if err := ValidateToolName(toolName); err != nil {
			http.Error(w, "invalid tool name", http.StatusBadRequest)
			return
		}

		// Verificar allowlist
		if !allowedTools[toolName] {
			http.Error(w, "tool not allowed", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	tests := map[string]int{
		"/mcp/echo":       http.StatusOK,
		"/mcp/filesystem": http.StatusOK,
		"/mcp/git":        http.StatusOK,
		"/mcp/whoami":     http.StatusNotFound,   // não está na allowlist
		"/mcp/bash":       http.StatusNotFound,   // não está na allowlist
		"/mcp/../../etc":  http.StatusBadRequest, // inválido
	}

	for path, expectStatus := range tests {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(server.URL + path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != expectStatus {
				t.Errorf("path %q: expected %d, got %d", path, expectStatus, resp.StatusCode)
			}
		})
	}
}

// TestPathTraversalInWorkspace verifica que path traversal é bloqueado
// em qualquer argumentos de workspace
func TestPathTraversalInWorkspace(t *testing.T) {
	tmpdir := t.TempDir()

	tests := map[string]bool{
		"valid.txt":           true,
		"subdir/file.txt":     true,
		"../../../etc/passwd": false,
		"%2e%2e/etc/passwd":   false,
		"...//etc/passwd":     false,
		"/etc/passwd":         false,
	}

	for path, shouldSucceed := range tests {
		t.Run(path, func(t *testing.T) {
			_, err := ValidatePath(tmpdir, path)
			if shouldSucceed && err != nil {
				t.Errorf("expected success for %q, got error: %v", path, err)
			}
			if !shouldSucceed && err == nil {
				t.Errorf("expected error for %q, got nil", path)
			}
		})
	}
}

// TestEncodingBypass_Multiple verifica vários níveis de encoding
func TestEncodingBypass_Multiple(t *testing.T) {
	tmpdir := t.TempDir()

	// Encoding: ../ = %2e%2e%2f
	// Double encoding: %2e%2e%2f = %252e%252e%252f
	// Triple encoding: %252e%252e%252f = %25252e%25252e%25252f

	encodings := []string{
		"../",
		"%2e%2e%2f",
		"%2E%2E%2F",       // uppercase
		"%252e%252e%252f", // double
	}

	for _, enc := range encodings {
		t.Run(enc, func(t *testing.T) {
			_, err := ValidatePath(tmpdir, enc)
			if err == nil {
				t.Errorf("should reject encoding variant: %q", enc)
			}
		})
	}
}

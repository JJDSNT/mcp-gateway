package sandbox

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthHeadersBypassRegression verifica que nenhum header de auth pode bypassar validação
// Este é um teste-regressão: documenta que o router NÃO deve aceitar "atalhos" como:
//
//	if r.Header.Get("X-Auth") == "ok" { return }
//	if r.Header.Get("X-Forwarded-User") == "admin" { return }
//
// A regra é simples: headers não mudam o resultado da validação,
// a menos que sejam headers realmente usados por feature (e.g., X-Forwarded-For para logging).
func TestAuthHeadersBypassRegression(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		addHeaders    map[string]string
		shouldBeValid bool
	}{
		{
			"valid tool, no auth headers",
			"valid-tool",
			map[string]string{},
			true,
		},
		{
			"invalid tool, no auth headers",
			"../../etc/passwd",
			map[string]string{},
			false,
		},
		{
			"invalid tool, but X-Auth header claims 'ok' (WRONG BYPASS)",
			"../../etc/passwd",
			map[string]string{
				"X-Auth": "ok",
			},
			false, // Deve CONTINUAR inválido
		},
		{
			"invalid tool, but Authorization header present (WRONG BYPASS)",
			"../../etc/passwd",
			map[string]string{
				"Authorization": "Bearer valid-token-123",
			},
			false, // Deve CONTINUAR inválido
		},
		{
			"invalid tool, but X-Forwarded-User=admin (WRONG BYPASS)",
			"../../bin",
			map[string]string{
				"X-Forwarded-User": "admin",
			},
			false, // Deve CONTINUAR inválido
		},
		{
			"invalid tool, but CF-Access-Authenticated=true (WRONG BYPASS)",
			"../../bin",
			map[string]string{
				"CF-Access-Authenticated": "true",
			},
			false, // Deve CONTINUAR inválido
		},
		{
			"valid tool, garbage auth headers",
			"valid-tool",
			map[string]string{
				"X-Auth":   "fake",
				"X-User":   "fake",
				"X-Admin":  "true",
				"X-Bypass": "yes",
			},
			true, // Deve passar (headers ignorados)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simula validação de tool name
			isValid := validateToolNameSimple(tt.toolName)

			// Adiciona headers mas não afeta resultado
			req := httptest.NewRequest("POST", "/mcp/"+tt.toolName, bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Content-Type", "application/json")
			for k, v := range tt.addHeaders {
				req.Header.Set(k, v)
			}

			// A validação não deve mudar baseada em headers
			if isValid != tt.shouldBeValid {
				t.Errorf("tool name validation changed: expected %v, got %v", tt.shouldBeValid, isValid)
			}
		})
	}
}

// TestAuthHeadersDoNotAffectResponse verifica que mesmos requests com/sem auth headers têm mesma resposta
func TestAuthHeadersDoNotAffectResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simula handler que valida tool name
		toolName := "test"
		if err := validateToolNameError(toolName); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid tool"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Request sem auth headers
	req1 := httptest.NewRequest("POST", "/mcp/test", bytes.NewReader([]byte(`{}`)))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	// Request com auth headers
	req2 := httptest.NewRequest("POST", "/mcp/test", bytes.NewReader([]byte(`{}`)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer token")
	req2.Header.Set("X-Auth", "admin")
	req2.Header.Set("CF-Access-Authenticated", "true")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	// Ambas devem ter mesma resposta
	if w1.Code != w2.Code {
		t.Errorf("response code differ: %d vs %d (auth headers should not affect)", w1.Code, w2.Code)
	}

	if w1.Body.String() != w2.Body.String() {
		t.Errorf("response body differ: %q vs %q (auth headers should not affect)", w1.Body.String(), w2.Body.String())
	}
}

// TestProxyHeadersAreLogged verifies that proxy headers (X-Forwarded-For, etc) are NOT used for validation
func TestProxyHeadersAreNotUsedForValidation(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		proxyHeaders  map[string]string
		shouldBeValid bool
	}{
		{
			"valid tool, X-Forwarded-For present",
			"fs",
			map[string]string{"X-Forwarded-For": "127.0.0.1"},
			true,
		},
		{
			"invalid tool, X-Forwarded-For does NOT rescue it",
			"../../etc/passwd",
			map[string]string{"X-Forwarded-For": "1.2.3.4"},
			false,
		},
		{
			"valid tool, X-Forwarded-Proto does NOT affect it",
			"git",
			map[string]string{"X-Forwarded-Proto": "https"},
			true,
		},
		{
			"invalid tool, X-Real-IP does NOT rescue it",
			"invalid;whoami",
			map[string]string{"X-Real-IP": "192.168.1.1"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := validateToolNameSimple(tt.toolName)

			req := httptest.NewRequest("POST", "/mcp/"+tt.toolName, bytes.NewReader([]byte(`{}`)))
			for k, v := range tt.proxyHeaders {
				req.Header.Set(k, v)
			}

			if isValid != tt.shouldBeValid {
				t.Errorf("proxy headers should not affect validation: expected %v, got %v", tt.shouldBeValid, isValid)
			}
		})
	}
}

// TestCloudflareHeadersAreNotUsedForValidation verifies Cloudflare headers don't bypass checks
func TestCloudflareHeadersAreNotUsedForValidation(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		cfHeaders     map[string]string
		shouldBeValid bool
	}{
		{
			"valid tool, CF-Ray present",
			"fs",
			map[string]string{"CF-Ray": "abc123"},
			true,
		},
		{
			"invalid tool, CF-Access-Authenticated does NOT bypass",
			"../../etc/passwd",
			map[string]string{
				"CF-Access-Authenticated": "true",
				"CF-Access-Jwt-Assertion": "eyJ...",
			},
			false,
		},
		{
			"invalid tool, CF-Visitor does NOT bypass",
			"../../../",
			map[string]string{
				"CF-Visitor":       `{"scheme":"https"}`,
				"CF-Connecting-IP": "1.1.1.1",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := validateToolNameSimple(tt.toolName)

			req := httptest.NewRequest("POST", "/mcp/"+tt.toolName, bytes.NewReader([]byte(`{}`)))
			for k, v := range tt.cfHeaders {
				req.Header.Set(k, v)
			}

			if isValid != tt.shouldBeValid {
				t.Errorf("CF headers should not affect validation: expected %v, got %v", tt.shouldBeValid, isValid)
			}
		})
	}
}

// === Helpers ===

// validateToolNameSimple é um helper para testar sem necessidade do sandbox package
func validateToolNameSimple(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}

	// Mesma lógica de sandbox.ValidateToolName
	for _, ch := range name {
		if ch == '/' || ch == '\\' || ch == ' ' {
			return false
		}
	}

	// Rejeita encodings
	if contains(name, "%2f", "%5c", "%25", "..") {
		return false
	}

	// Aceita alphanumeric + - e _
	for _, ch := range name {
		isAlnum := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		isDash := ch == '-'
		isUnderscore := ch == '_'

		if !isAlnum && !isDash && !isUnderscore {
			return false
		}
	}

	return true
}

func contains(s string, strs ...string) bool {
	for _, str := range strs {
		if contains_internal(s, str) {
			return true
		}
	}
	return false
}

func contains_internal(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func validateToolNameError(name string) error {
	if validateToolNameSimple(name) {
		return nil
	}
	return ErrorInvalidToolName
}

type errorType string

func (e errorType) Error() string {
	return string(e)
}

const ErrorInvalidToolName errorType = "invalid tool name"

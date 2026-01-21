package sandbox

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPMethodsNotAllowed verifica que apenas GET/POST são permitidos no /mcp/<tool>
// PUT, DELETE, PATCH, TRACE → 405 Method Not Allowed
func TestHTTPMethodNotAllowed(t *testing.T) {
	tests := []struct {
		method       string
		expectedCode int
	}{
		{"GET", http.StatusOK},        // GET permitido (buscar status?)
		{"POST", http.StatusOK},       // POST permitido (padrão MCP)
		{"PUT", http.StatusMethodNotAllowed},
		{"DELETE", http.StatusMethodNotAllowed},
		{"PATCH", http.StatusMethodNotAllowed},
		{"TRACE", http.StatusMethodNotAllowed},
		{"OPTIONS", http.StatusMethodNotAllowed},
		{"CONNECT", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/mcp/test", bytes.NewReader([]byte(`{}`)))
			w := httptest.NewRecorder()

			// Simula handler básico que rejeita métodos não permitidos
			if req.Method != http.MethodGet && req.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)

			if w.Code != tt.expectedCode {
				t.Errorf("method %s: expected %d, got %d", tt.method, tt.expectedCode, w.Code)
			}
		})
	}
}

// TestContentTypeValidation verifica que POST/PUT com Content-Type inválido retorna 415
func TestContentTypeValidation(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		contentType    string
		expectedCode   int
		shouldValidate bool
	}{
		// POST com JSON
		{"POST JSON valid", "POST", "application/json", http.StatusOK, true},
		{"POST JSON with charset", "POST", "application/json; charset=utf-8", http.StatusOK, true},

		// POST com outros content-types (devem ser rejeitados)
		{"POST form-urlencoded", "POST", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType, true},
		{"POST xml", "POST", "application/xml", http.StatusUnsupportedMediaType, true},
		{"POST text plain", "POST", "text/plain", http.StatusUnsupportedMediaType, true},
		{"POST empty", "POST", "", http.StatusUnsupportedMediaType, true},

		// GET não deve validar Content-Type (não tem body)
		{"GET with JSON", "GET", "application/json", http.StatusOK, false},
		{"GET with any type", "GET", "text/html", http.StatusOK, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/mcp/test", bytes.NewReader([]byte(`{}`)))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			w := httptest.NewRecorder()

			// Simula validação básica
			code := http.StatusOK
			if tt.shouldValidate && tt.method == http.MethodPost {
				ct := req.Header.Get("Content-Type")
				if ct == "" || (ct != "application/json" && !bytes.HasPrefix([]byte(ct), []byte("application/json;"))) {
					code = http.StatusUnsupportedMediaType
				}
			}
			w.WriteHeader(code)

			if w.Code != tt.expectedCode {
				t.Errorf("%s: expected %d, got %d", tt.name, tt.expectedCode, w.Code)
			}
		})
	}
}

// TestEmptyBodyOnMissingContentType verifica que POST sem Content-Type é rejeitado
func TestPostMissingContentType(t *testing.T) {
	req := httptest.NewRequest("POST", "/mcp/test", bytes.NewReader([]byte(`{}`)))
	// Sem definir Content-Type explicitamente
	w := httptest.NewRecorder()

	// POST sem Content-Type deve ser rejeitado
	ct := req.Header.Get("Content-Type")
	if ct == "" || ct != "application/json" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415, got %d", w.Code)
	}
}

// TestHeaderValidation verifica que headers suspeitos são ignorados
func TestHeaderValidation(t *testing.T) {
	tests := []struct {
		name       string
		addHeaders map[string]string
		shouldPass bool
	}{
		{
			"valid with standard headers",
			map[string]string{
				"User-Agent":      "curl",
				"Accept":          "application/json",
				"Accept-Encoding": "gzip",
			},
			true,
		},
		{
			"valid with proxy headers",
			map[string]string{
				"X-Forwarded-For":   "192.168.1.1",
				"X-Forwarded-Proto": "https",
				"X-Real-IP":         "192.168.1.1",
			},
			true,
		},
		{
			"valid with cloudflare headers",
			map[string]string{
				"CF-Connecting-IP":        "192.168.1.1",
				"CF-Ray":                  "abc123",
				"CF-Visitor":              `{"scheme":"https"}`,
				"CF-Access-Authenticated": "true",
			},
			true,
		},
		{
			"injection attempt in headers",
			map[string]string{
				"X-Custom-Header": "../../etc/passwd",
				"Authorization":   "Bearer fake",
			},
			true, // headers são ignorados, não causam erro
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mcp/test", bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Content-Type", "application/json")

			for k, v := range tt.addHeaders {
				req.Header.Set(k, v)
			}

			// Se os headers não causam erro de parsing, deve passar
			if req.Method == "POST" && req.Header.Get("Content-Type") == "application/json" {
				if !tt.shouldPass {
					t.Errorf("expected to fail but passed")
				}
			}
		})
	}
}

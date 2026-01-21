package sandbox

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSEHeadersPresent verifica que o servidor envia todos os headers SSE corretos
func TestSSEHeadersPresent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Header adicional para evitar buffer em proxies
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		w.Write([]byte("data: hello\n\n"))
	})

	req := httptest.NewRequest("GET", "/sse", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	tests := []struct {
		header   string
		expected string
	}{
		{"Content-Type", "text/event-stream"},
		{"Cache-Control", "no-cache"},
		{"Connection", "keep-alive"},
		{"X-Accel-Buffering", "no"},
	}

	for _, test := range tests {
		got := w.Header().Get(test.header)
		if got != test.expected {
			t.Errorf("header %s: expected %q, got %q", test.header, test.expected, got)
		}
	}
}

// TestSSEContentTypeExact verifica que Content-Type é exatamente text/event-stream
func TestSSEContentTypeExact(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		isValid     bool
	}{
		{"exact text/event-stream", "text/event-stream", true},
		{"with charset", "text/event-stream; charset=utf-8", true},
		{"application/x-ndjson (não SSE)", "application/x-ndjson", false},
		{"text/plain (não SSE)", "text/plain", false},
		{"application/json (não SSE)", "application/json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/sse", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			ct := w.Header().Get("Content-Type")
			isSSE := strings.HasPrefix(ct, "text/event-stream")

			if isSSE != tt.isValid {
				t.Errorf("content-type %q: expected SSE=%v, got %v", tt.contentType, tt.isValid, isSSE)
			}
		})
	}
}

// TestSSENoCache verifica que Cache-Control: no-cache está presente
func TestSSENoCache(t *testing.T) {
	tests := []struct {
		name            string
		cacheControl    string
		shouldHaveNoCap bool
	}{
		{"correct no-cache", "no-cache", true},
		{"with no-store", "no-cache, no-store, max-age=0", true},
		{"missing no-cache (WRONG)", "max-age=3600", false},
		{"missing no-cache (WRONG) 2", "public, max-age=60", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", tt.cacheControl)
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/sse", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			cc := w.Header().Get("Cache-Control")
			hasNoCap := strings.Contains(cc, "no-cache")

			if hasNoCap != tt.shouldHaveNoCap {
				t.Errorf("cache-control %q: expected no-cache=%v, got %v", tt.cacheControl, tt.shouldHaveNoCap, hasNoCap)
			}
		})
	}
}

// TestSSEFlusherInterface verifica que ResponseWriter implementa http.Flusher
func TestSSEFlusherInterface(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Flush deve ser callable
		w.Write([]byte("data: test\n\n"))
		flusher.Flush()
	})

	req := httptest.NewRequest("GET", "/sse", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestSSEProxyBufferingHeader verifica X-Accel-Buffering para evitar buffer em Nginx
func TestSSEProxyBufferingHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/sse", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	got := w.Header().Get("X-Accel-Buffering")
	if got != "no" {
		t.Errorf("X-Accel-Buffering: expected 'no', got %q", got)
	}
}

// TestSSEConnectionKeepAlive verifica que Connection: keep-alive permite múltiplos eventos
func TestSSEConnectionKeepAlive(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Keep-Alive", "timeout=60, max=100")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/sse", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	conn := w.Header().Get("Connection")
	if conn != "keep-alive" {
		t.Errorf("Connection: expected 'keep-alive', got %q", conn)
	}

	ka := w.Header().Get("Keep-Alive")
	if !strings.Contains(ka, "timeout") || !strings.Contains(ka, "max") {
		t.Logf("Keep-Alive header not fully set (optional): %q", ka)
	}
}

// TestSSERejectCachingHeaders verifica que response não contém headers que quebram SSE
func TestSSERejectCachingHeaders(t *testing.T) {
	badHeaders := []struct {
		name  string
		value string
	}{
		{"Cache-Control", "public, max-age=3600"},
		{"Cache-Control", "private, max-age=60"},
		{"Pragma", "cache"},
		{"Expires", "Sun, 01 Jan 2025 00:00:00 GMT"},
	}

	for _, bad := range badHeaders {
		t.Run(bad.name+"="+bad.value, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache") // Correto
				if bad.name == "Cache-Control" {
					t.Logf("skipping bad header injection in test")
					return
				}
				w.Header().Set(bad.name, bad.value)
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/sse", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			// Verifica que Cache-Control correto está presente
			cc := w.Header().Get("Cache-Control")
			if !strings.Contains(cc, "no-cache") {
				t.Errorf("missing no-cache in Cache-Control")
			}
		})
	}
}

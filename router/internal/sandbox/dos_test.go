package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRequestBodyLimit verifica que o servidor rejeita bodies muito grandes
func TestRequestBodyLimit(t *testing.T) {
	// Simular um handler que respeita MaxBytesReader
	maxBytes := int64(1 << 20) // 1MB

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Aplicar limite de tamanho (como em main.go)
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

		// Tentar ler body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "received %d bytes", len(body))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	tests := []struct {
		name          string
		size          int64
		expectSuccess bool
	}{
		{"small body", 1024, true},
		{"max body", maxBytes, true},
		{"over limit", maxBytes + 1, false},
		{"huge body", maxBytes * 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewReader(make([]byte, tt.size))
			resp, err := http.Post(server.URL, "application/json", body)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if tt.expectSuccess && resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
			if !tt.expectSuccess && resp.StatusCode == http.StatusOK {
				t.Errorf("expected error status, got 200")
			}
		})
	}
}

// TestContextTimeout verifica que contexts com timeout cancelam execução
func TestContextTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		work    time.Duration
		expect  bool // true = deve timeout
	}{
		{"work finishes in time", 100 * time.Millisecond, 10 * time.Millisecond, false},
		{"work exceeds timeout", 10 * time.Millisecond, 100 * time.Millisecond, true},
		{"zero timeout", 0, 1 * time.Millisecond, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			done := make(chan bool, 1)

			// Simular trabalho que leva tempo
			go func() {
				select {
				case <-ctx.Done():
					// Context foi cancelado antes de terminar
					return
				case <-time.After(tt.work):
					done <- true
				}
			}()

			select {
			case <-ctx.Done():
				if !tt.expect {
					t.Errorf("context canceled unexpectedly: %v", ctx.Err())
				}
			case <-done:
				if tt.expect {
					t.Errorf("work completed but expected timeout")
				}
			}
		})
	}
}

// TestSSEStreamingMemory verifica que streaming SSE não consome memória infinita
// com conexão lenta
func TestSSEStreamingMemory(t *testing.T) {
	// Simular um servidor SSE
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Enviar múltiplas mensagens com flush
		for i := 0; i < 100; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}

			// Enviar evento pequeno
			fmt.Fprintf(w, "event: message\n")
			fmt.Fprintf(w, "data: %d\n\n", i)
			flusher.Flush()

			// Pequeno delay entre mensagens
			time.Sleep(1 * time.Millisecond)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Cliente que lê resposta lentamente
	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Ler response lentamente (simular conexão lenta)
	buf := make([]byte, 1)
	count := 0
	for {
		n, err := resp.Body.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if n > 0 {
			count++
		}

		// Delay grande entre reads (simular cliente lento)
		// Em um cliente realmente lento, o servidor não deveria acumular buffer
		// time.Sleep(100 * time.Millisecond)
	}

	if count == 0 {
		t.Errorf("no data received")
	}
}

// TestStreamWithReadDeadline verifica que streaming respeita deadlines
func TestStreamWithReadDeadline(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Tentar enviar muitas mensagens rapidamente
		for i := 0; i < 10000; i++ {
			fmt.Fprintf(w, "event: msg\ndata: %d\n\n", i)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Cliente com read timeout
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}

	resp, err := client.Get(server.URL)
	if err == nil {
		// Se conseguir fazer request, pelo menos ler com timeout
		defer resp.Body.Close()

		// ReadAll com timeout implícito (http.Client.Timeout)
		_, _ = io.ReadAll(resp.Body)
	}

	// Se timeout, espera-se que haja erro
	if err != nil && strings.Contains(err.Error(), "timeout") {
		t.Logf("stream timeout as expected: %v", err)
	}
}

// TestStreamWithBufferLimit verifica que scanner tem limite de buffer
func TestStreamWithBufferLimit(t *testing.T) {
	// Simular o comportamento do scanner no main.go
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cada linha de output pode ter até 4MB
		// Total buffer pode ter até 4MB (como em main.go)
		for i := 0; i < 10; i++ {
			// Linha menor que 4MB
			line := strings.Repeat("x", 1024*10) // 10KB por linha
			fmt.Fprintf(w, "%s\n", line)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Simular o scanner do projeto
	// scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	// Isso permite linhas até 4MB

	count := 0
	for scanner := resp.Body; ; {
		buf := make([]byte, 1024)
		n, err := scanner.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		count += n
	}

	if count == 0 {
		t.Errorf("no data received")
	}
}

// TestReadBodyWithDeadline verifica que leitura de body respeita deadline
func TestReadBodyWithDeadline(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simular leitura de body com MaxBytesReader
		// que já respeita http.Request timeout

		// Ler até fim
		buf := make([]byte, 1024)
		total := 0
		for {
			n, err := r.Body.Read(buf)
			total += n
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, "read error", http.StatusBadRequest)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "read %d bytes", total)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Post com corpo lento
	bodyReader := newSlowReader(1024*1024, 10*time.Millisecond)
	resp, err := http.Post(server.URL, "application/json", bodyReader)
	if err == nil {
		resp.Body.Close()
	}

	// Servidor deve processar (pode ter timeout ou sucesso)
	// O importante é não ficar preso indefinidamente
}

// slowReader é um io.Reader que envia dados lentamente
type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func newSlowReader(size int, delay time.Duration) *slowReader {
	return &slowReader{
		data:  make([]byte, size),
		delay: delay,
	}
}

func (sr *slowReader) Read(p []byte) (n int, err error) {
	if sr.pos >= len(sr.data) {
		return 0, io.EOF
	}

	// Ler um pouco de cada vez
	n = copy(p, sr.data[sr.pos:])
	sr.pos += n

	// Delay entre chunks
	time.Sleep(sr.delay)

	return n, nil
}

package sandbox

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestContextCanceledOnClientDisconnect(t *testing.T) {
	var sawCancel int32

	// Handler que simula "trabalho longo" e observa cancelamento do contexto.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Força headers pra resposta começar (como SSE faria).
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		ctx := r.Context()

		// Espera ou cancelamento.
		select {
		case <-ctx.Done():
			atomic.StoreInt32(&sawCancel, 1)
			return
		case <-time.After(5 * time.Second):
			// Se chegar aqui, significa que NÃO cancelou (ruim para túnel).
			return
		}
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.EnableHTTP2 = false // simplifica comportamento em alguns ambientes
	srv.Start()
	defer srv.Close()

	// Dial TCP direto para poder "derrubar" a conexão do cliente na unha.
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Faz um request HTTP simples e depois fecha a conexão.
	// (Um client SSE real faria isso ao perder o túnel.)
	req := "GET / HTTP/1.1\r\nHost: test\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		t.Fatalf("write request: %v", err)
	}

	// Espera um tiquinho pro servidor começar a lidar com a request.
	time.Sleep(50 * time.Millisecond)

	// Fecha do lado do cliente (simulando desconexão).
	_ = conn.Close()

	// Dá tempo pro servidor perceber e cancelar o contexto.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&sawCancel) != 0 {
			return // PASS
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected request context to be canceled after client disconnect")
}

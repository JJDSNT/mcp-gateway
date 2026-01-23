package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mcp-router/internal/shim"
)

type config struct {
	Endpoint  string
	Timeout   time.Duration
	Debug     bool
	RequestID string
}

func main() {
	cfg := parseFlags()

	rid := strings.TrimSpace(cfg.RequestID)
	if rid == "" {
		rid = shim.NewRequestID()
	}

	level := shim.ParseLogLevelFromEnv()
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := shim.NewLogger(shim.LogConfig{
		Mode:      shim.ParseLogModeFromEnv(),
		Level:     level,
		Component: "shim-xport",
	}).With(
		slog.String("endpoint", cfg.Endpoint),
		shim.RequestID(rid),
	)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := run(ctx, cfg, rid, logger); err != nil {
		logger.Error("fatal", shim.Err(err))
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.Endpoint, "endpoint", "", "HTTP endpoint MCP (ex: http://localhost:8080/mcp/echo)")
	flag.DurationVar(&cfg.Timeout, "timeout", 0, "Timeout HTTP (0 = sem timeout)")
	flag.BoolVar(&cfg.Debug, "debug", false, "Habilita debug (override de SHIM_LOG_LEVEL)")
	flag.StringVar(&cfg.RequestID, "request-id", "", "Request ID para correlação (opcional; se vazio, gera)")
	flag.Parse()

	if cfg.Endpoint == "" {
		fmt.Fprintln(os.Stderr, "missing --endpoint")
		os.Exit(2)
	}
	return cfg
}

func run(ctx context.Context, cfg config, rid string, log *slog.Logger) error {
	start := time.Now()

	log.Info("starting",
		slog.Int64("timeout_ms", cfg.Timeout.Milliseconds()),
	)

	// Canal que liga STDIN -> HTTP body
	pr, pw := io.Pipe()

	// stdin -> pipe (linha a linha). Não loga payload; no debug, loga tamanho.
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			b := scanner.Bytes()

			_, _ = pw.Write(append(b, '\n'))

			if log.Enabled(ctx, slog.LevelDebug) {
				log.Debug("stdin -> http",
					slog.Int("bytes", len(b)),
				)
			}
		}
		if err := scanner.Err(); err != nil {
			log.Warn("stdin scanner error", shim.Err(err))
		}
		_ = pw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, pr)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")

	// 🔑 Correlaciona shim -> gateway/router
	req.Header.Set("X-Request-Id", rid)

	client := &http.Client{Timeout: cfg.Timeout}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	//nolint:errcheck
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(ct, "text/event-stream")

	log.Info("connected",
		slog.String("status", resp.Status),
		slog.Int("status_code", resp.StatusCode),
		slog.String("content_type", ct),
		slog.Bool("is_sse", isSSE),
	)

	// Se HTTP status não é 2xx, lê snippet e retorna erro.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodySnippet := readSnippet(resp.Body, 2048)
		err := fmt.Errorf("non-2xx response: %s body=%q", resp.Status, bodySnippet)
		log.Error("upstream error",
			shim.Err(err),
			shim.DurationMs(time.Since(start).Milliseconds()),
		)
		return err
	}

	var consumeErr error
	if isSSE {
		consumeErr = consumeSSE(ctx, resp.Body, log)
	} else {
		consumeErr = consumeStream(ctx, resp.Body, log)
	}

	if consumeErr != nil {
		log.Warn("stream ended with error",
			shim.Err(consumeErr),
			shim.DurationMs(time.Since(start).Milliseconds()),
		)
		return consumeErr
	}

	log.Info("stopped",
		shim.DurationMs(time.Since(start).Milliseconds()),
	)
	return nil
}

func consumeStream(ctx context.Context, r io.Reader, log *slog.Logger) error {
	reader := bufio.NewReader(r)
	var bytesOut int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')

		if len(bytes.TrimSpace(line)) > 0 {
			_, _ = os.Stdout.Write(line)
			bytesOut += int64(len(line))

			if log.Enabled(ctx, slog.LevelDebug) {
				log.Debug("http -> stdout",
					slog.Int("bytes", len(line)),
					slog.Int64("bytes_out_total", bytesOut),
				)
			}
		}

		if err != nil {
			if err == io.EOF {
				if log.Enabled(ctx, slog.LevelDebug) {
					log.Debug("http stream EOF", slog.Int64("bytes_out_total", bytesOut))
				}
				return nil
			}
			return err
		}
	}
}

func consumeSSE(ctx context.Context, r io.Reader, log *slog.Logger) error {
	scanner := bufio.NewScanner(r)

	const maxToken = 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxToken)

	var bytesOut int64

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			if payload == "[DONE]" {
				if log.Enabled(ctx, slog.LevelDebug) {
					log.Debug("sse done", slog.Int64("bytes_out_total", bytesOut))
				}
				return nil
			}

			out := []byte(payload + "\n")
			_, _ = os.Stdout.Write(out)
			bytesOut += int64(len(out))

			if log.Enabled(ctx, slog.LevelDebug) {
				log.Debug("sse -> stdout",
					slog.Int("bytes", len(out)),
					slog.Int64("bytes_out_total", bytesOut),
				)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func readSnippet(r io.Reader, n int) string {
	b, err := io.ReadAll(io.LimitReader(r, int64(n)))
	if err != nil {
		return ""
	}
	return string(b)
}

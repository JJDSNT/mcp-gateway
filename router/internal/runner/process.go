package runner

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"mcp-router/internal/observability/logging"
)

type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() error
	Close() error
}

type execProcess struct {
	toolName string
	runtime  string

	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	log *slog.Logger

	startedAt time.Time

	closeOnce sync.Once
	wg        sync.WaitGroup
	closeFn   func()
	waitFn    func() error
}

func (p *execProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *execProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *execProcess) Stderr() io.ReadCloser { return p.stderr }

// Wait espera o processo terminar e registra sucesso/erro + duração.
// Não loga stdout/payload.
func (p *execProcess) Wait() error {
	start := p.startedAt
	if start.IsZero() {
		start = time.Now()
	}

	err := p.waitFn()

	dur := time.Since(start).Milliseconds()

	if err != nil {
		p.logger().Warn("process exited with error",
			logging.DurationMs(dur),
			logging.Err(err),
		)
		return err
	}

	p.logger().Info("process exited",
		logging.DurationMs(dur),
	)
	return nil
}

// Close mata o processo (idempotente), espera pumps terminarem e registra duração.
func (p *execProcess) Close() error {
	start := time.Now()

	p.closeOnce.Do(func() {
		p.logger().Debug("closing process (kill)",
			logging.Tool(p.toolName),
			logging.Runtime(p.runtime),
		)
		p.closeFn()
	})

	// Aguarda stderr pump (e outros goroutines owned pelo process)
	p.wg.Wait()

	p.logger().Debug("process closed",
		logging.DurationMs(time.Since(start).Milliseconds()),
	)

	return nil
}

// startStderrPump faz streaming do stderr da tool para logs estruturados.
//
// Regras:
// - nunca escreve no stdout
// - respeita ctx.Done()
// - logs em nível Debug (stderr pode ser barulhento)
// - proteção simples contra spam: trunca após N linhas
func (p *execProcess) startStderrPump(ctx context.Context) {
	if p.stderr == nil {
		return
	}

	log := p.logger()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		pumpStart := time.Now()

		sc := bufio.NewScanner(p.stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		const maxLines = 5000
		lines := 0
		truncated := false

		log.Debug("stderr pump started")

		for sc.Scan() {
			select {
			case <-ctx.Done():
				log.Debug("stderr pump context cancelled",
					logging.Err(ctx.Err()),
					logging.DurationMs(time.Since(pumpStart).Milliseconds()),
				)
				return
			default:
			}

			lines++
			if lines <= maxLines {
				log.Debug("tool stderr",
					slog.String("stderr", sc.Text()),
					slog.Int("stderr_line", lines),
				)
				continue
			}

			// Passou do limite: loga um único aviso e para de emitir linhas
			if !truncated {
				truncated = true
				log.Warn("tool stderr truncated",
					slog.Int("max_lines", maxLines),
				)
			}
		}

		if err := sc.Err(); err != nil {
			log.Warn("stderr scanner error",
				logging.Err(err),
				logging.DurationMs(time.Since(pumpStart).Milliseconds()),
			)
			return
		}

		log.Debug("stderr pump finished",
			slog.Int("stderr_lines", lines),
			slog.Bool("stderr_truncated", truncated),
			logging.DurationMs(time.Since(pumpStart).Milliseconds()),
		)
	}()
}

// logger retorna um logger estruturado com campos fixos.
// - se p.log já foi injetado no Start(), usa ele
// - senão, cai para slog.Default() (ainda com fields básicos)
func (p *execProcess) logger() *slog.Logger {
	if p.log != nil {
		// garante que os campos fixos existam
		return p.log.With(
			logging.Tool(p.toolName),
			logging.Runtime(p.runtime),
		)
	}

	// fallback (sem request_id)
	return slog.Default().With(
		logging.Tool(p.toolName),
		logging.Runtime(p.runtime),
	)
}

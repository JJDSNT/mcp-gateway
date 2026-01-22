package shim

import (
	"context"
	"io"
	"sync"
)

// PipeConfig controla o comportamento do piping
type PipeConfig struct {
	// Se true, fecha o writer quando o reader termina
	CloseWriter bool

	// Callback opcional para erros (nil = ignora)
	OnError func(error)
}

// Pipe copia dados de r para w até EOF, erro ou cancelamento do contexto.
// É segura para uso em goroutines.
func Pipe(ctx context.Context, r io.Reader, w io.Writer, cfg PipeConfig) {
	go func() {
		defer func() {
			if cfg.CloseWriter {
				if c, ok := w.(io.Closer); ok {
					_ = c.Close()
				}
			}
		}()

		buf := make([]byte, 32*1024)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := r.Read(buf)
				if n > 0 {
					if _, werr := w.Write(buf[:n]); werr != nil {
						if cfg.OnError != nil {
							cfg.OnError(werr)
						}
						return
					}
				}
				if err != nil {
					if err != io.EOF && cfg.OnError != nil {
						cfg.OnError(err)
					}
					return
				}
			}
		}
	}()
}

// PipeBoth conecta dois pares de streams em paralelo.
// Útil para STDIN/STDOUT/STDERR pass-through.
func PipeBoth(
	ctx context.Context,
	aIn io.Reader, aOut io.Writer,
	bIn io.Reader, bOut io.Writer,
) {
	Pipe(ctx, aIn, bOut, PipeConfig{CloseWriter: true})
	Pipe(ctx, bIn, aOut, PipeConfig{CloseWriter: true})
}

// PipeGroup permite esperar vários pipes terminarem.
type PipeGroup struct {
	wg sync.WaitGroup
}

// Go inicia um pipe rastreável.
func (g *PipeGroup) Go(
	ctx context.Context,
	r io.Reader,
	w io.Writer,
	cfg PipeConfig,
) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		Pipe(ctx, r, w, cfg)
	}()
}

// Wait aguarda todos os pipes terminarem.
func (g *PipeGroup) Wait() {
	g.wg.Wait()
}

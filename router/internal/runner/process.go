package runner

import (
	"bufio"
	"context"
	"io"
	"log"
	"sync"
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

	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	closeOnce sync.Once
	wg        sync.WaitGroup
	closeFn   func()
	waitFn    func() error
}

func (p *execProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *execProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *execProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *execProcess) Wait() error           { return p.waitFn() }

func (p *execProcess) Close() error {
	p.closeOnce.Do(p.closeFn)
	p.wg.Wait()
	return nil
}

func (p *execProcess) startStderrPump(ctx context.Context) {
	if p.stderr == nil {
		return
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		sc := bufio.NewScanner(p.stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		for sc.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			log.Printf("[tool=%s stderr] %s", p.toolName, sc.Text())
		}
		if err := sc.Err(); err != nil {
			log.Printf("[tool=%s stderr] scanner error: %v", p.toolName, err)
		}
	}()
}

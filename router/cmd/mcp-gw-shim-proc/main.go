package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mcp-router/internal/shim"
)

type config struct {
	Distro        string
	User          string
	Cwd           string
	Command       string
	ExtraWslArgs  string
	ShutdownGrace time.Duration
	Debug         bool
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "arg error:", err)
		os.Exit(2)
	}

	level := shim.ParseLogLevelFromEnv()
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := shim.NewLogger(shim.LogConfig{
		Mode:      shim.ParseLogModeFromEnv(),
		Level:     level,
		Component: "shim-proc",
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	code := run(ctx, cfg, logger)
	os.Exit(code)
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("mcp-gw-shim-proc", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&cfg.Distro, "distro", "", "Nome da distro WSL (ex: Ubuntu-22.04). Opcional.")
	fs.StringVar(&cfg.User, "user", "", "Usuário no WSL (opcional).")
	fs.StringVar(&cfg.Cwd, "cwd", "", "Diretório de trabalho no WSL (opcional).")
	fs.StringVar(&cfg.Command, "cmd", "", "Comando a executar no WSL (obrigatório). Ex: ./mcp-gw --config ./config.yaml")
	fs.StringVar(&cfg.ExtraWslArgs, "wsl-args", "", "Args extras para o wsl.exe (opcional). Ex: \"--exec\"")
	fs.DurationVar(&cfg.ShutdownGrace, "shutdown-grace", 1500*time.Millisecond, "Janela para shutdown gracioso.")
	fs.BoolVar(&cfg.Debug, "debug", false, "Habilita debug no stderr (override de SHIM_LOG_LEVEL).")

	if err := fs.Parse(args); err != nil {
		return cfg, fmt.Errorf("failed to parse flags: %w", err)
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return cfg, errors.New("missing --cmd")
	}
	return cfg, nil
}

func run(ctx context.Context, cfg config, log *slog.Logger) int {
	start := time.Now()

	bashScript := buildBashScript(cfg)

	wslArgs := buildWslArgs(cfg, bashScript)

	log.Info("starting",
		slog.String("distro", cfg.Distro),
		slog.String("user", cfg.User),
		slog.String("cwd", cfg.Cwd),
		slog.String("wsl_args", strings.Join(wslArgs, " ")),
	)

	cmd := exec.CommandContext(ctx, "wsl.exe", wslArgs...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Error("stdin pipe error", shim.Err(err))
		return 1
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error("stdout pipe error", shim.Err(err))
		return 1
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Error("stderr pipe error", shim.Err(err))
		return 1
	}

	if err := cmd.Start(); err != nil {
		log.Error("failed to start wsl.exe", shim.Err(err))
		return 1
	}

	// Piping (stdout deve permanecer "limpo")
	copyInDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdin, os.Stdin)
		_ = stdin.Close()
		close(copyInDone)
	}()

	copyOutDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(os.Stdout, stdout)
		close(copyOutDone)
	}()

	copyErrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(os.Stderr, stderr)
		close(copyErrDone)
	}()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		log.Warn("context cancelled, shutting down")

		select {
		case err := <-waitErr:
			code := exitCodeFromWait(err)
			log.Info("stopped",
				slog.Int("exit_code", code),
				shim.DurationMs(time.Since(start).Milliseconds()),
				shim.Err(err),
			)
			return code
		case <-time.After(cfg.ShutdownGrace):
			log.Warn("force killing wsl.exe",
				slog.Int64("grace_ms", cfg.ShutdownGrace.Milliseconds()),
			)
			_ = cmd.Process.Kill()
			err := <-waitErr
			code := exitCodeFromWait(err)
			log.Info("stopped",
				slog.Int("exit_code", code),
				shim.DurationMs(time.Since(start).Milliseconds()),
				shim.Err(err),
			)
			return code
		}

	case err := <-waitErr:
		// best-effort drains
		select {
		case <-copyInDone:
		default:
		}
		select {
		case <-copyOutDone:
		default:
		}
		select {
		case <-copyErrDone:
		default:
		}

		code := exitCodeFromWait(err)
		if err != nil {
			log.Warn("child exited with error",
				slog.Int("exit_code", code),
				shim.DurationMs(time.Since(start).Milliseconds()),
				shim.Err(err),
			)
		} else {
			log.Info("stopped",
				slog.Int("exit_code", code),
				shim.DurationMs(time.Since(start).Milliseconds()),
			)
		}
		return code
	}
}

func buildBashScript(cfg config) string {
	var sb strings.Builder
	sb.WriteString("set -euo pipefail; ")
	if cfg.Cwd != "" {
		sb.WriteString("cd ")
		sb.WriteString(shellQuote(cfg.Cwd))
		sb.WriteString("; ")
	}
	sb.WriteString(cfg.Command)
	return sb.String()
}

func buildWslArgs(cfg config, bashScript string) []string {
	wslArgs := []string{}

	if strings.TrimSpace(cfg.ExtraWslArgs) != "" {
		wslArgs = append(wslArgs, strings.Fields(cfg.ExtraWslArgs)...)
	}
	if cfg.Distro != "" {
		wslArgs = append(wslArgs, "-d", cfg.Distro)
	}
	if cfg.User != "" {
		wslArgs = append(wslArgs, "-u", cfg.User)
	}

	// "--" separa args do wsl.exe do comando Linux
	wslArgs = append(wslArgs, "--", "bash", "-lc", bashScript)
	return wslArgs
}

func exitCodeFromWait(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

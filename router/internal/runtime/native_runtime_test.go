package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"mcp-router/internal/config"
)

// Helper: permite que o próprio binário de teste se comporte como "tool".
func TestMain(m *testing.M) {
	// Se rodarmos como helper tool, executa e sai.
	if os.Getenv("MCP_ROUTER_TEST_HELPER") == "1" {
		helperMain()
		return
	}
	os.Exit(m.Run())
}

func helperMain() {
	// Protocolo simples:
	// args[1] = subcommand
	// - "echoargs": imprime os args após o subcommand, um por linha
	// - "printenv": imprime WORKSPACE_ROOT e TOOLS_ROOT
	// - "sleep": dorme até ser morto pelo contexto
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "missing subcommand")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "echoargs":
		for _, a := range os.Args[2:] {
			fmt.Fprintln(os.Stdout, a)
		}
		os.Exit(0)

	case "printenv":
		fmt.Fprintln(os.Stdout, os.Getenv("WORKSPACE_ROOT"))
		fmt.Fprintln(os.Stdout, os.Getenv("TOOLS_ROOT"))
		os.Exit(0)

	case "sleep":
		// Dorme “para sempre” (ou até receber kill do ctx).
		for {
			time.Sleep(200 * time.Millisecond)
		}

	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand:", os.Args[1])
		os.Exit(2)
	}
}

func TestNativeRuntime_Spawn_PassesArgsLiterally(t *testing.T) {
	cfg := &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
	}

	dangerous := []string{
		"; echo hacked",
		"| cat /etc/passwd",
		"&& rm -rf /",
		"$(whoami)",
		"`whoami`",
		"> /tmp/output",
	}

	tool := config.Tool{
		Cmd:  os.Args[0], // próprio binário de teste
		Args: append([]string{"echoargs"}, dangerous...),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rt := NativeRuntime{}
	cmd, _, stdout, stderr, err := rt.Spawn(ctx, cfg, tool)
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}
	defer cmd.Wait()

	outBytes, _ := io.ReadAll(stdout)
	errBytes, _ := io.ReadAll(stderr)
	if len(errBytes) > 0 {
		t.Logf("stderr: %s", string(errBytes))
	}

	lines := strings.Split(strings.TrimSpace(string(outBytes)), "\n")
	if len(lines) != len(dangerous) {
		t.Fatalf("expected %d lines, got %d. out=%q", len(dangerous), len(lines), string(outBytes))
	}

	for i, want := range dangerous {
		if lines[i] != want {
			t.Fatalf("arg %d mismatch: got %q want %q", i, lines[i], want)
		}
	}
}

func TestNativeRuntime_Spawn_SetsWorkspaceAndToolsEnv(t *testing.T) {
	cfg := &config.Config{
		WorkspaceRoot: "/workspaces",
		ToolsRoot:     "/tools",
	}

	tool := config.Tool{
		Cmd:  os.Args[0],
		Args: []string{"printenv"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rt := NativeRuntime{}
	cmd, _, stdout, stderr, err := rt.Spawn(ctx, cfg, tool)
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}
	defer cmd.Wait()

	outBytes, _ := io.ReadAll(stdout)
	errBytes, _ := io.ReadAll(stderr)
	if len(errBytes) > 0 {
		t.Logf("stderr: %s", string(errBytes))
	}

	lines := strings.Split(strings.TrimSpace(string(outBytes)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(outBytes))
	}

	if lines[0] != cfg.WorkspaceRoot {
		t.Fatalf("WORKSPACE_ROOT mismatch: got %q want %q", lines[0], cfg.WorkspaceRoot)
	}
	if lines[1] != cfg.ToolsRoot {
		t.Fatalf("TOOLS_ROOT mismatch: got %q want %q", lines[1], cfg.ToolsRoot)
	}
}

func TestNativeRuntime_Spawn_RespectsContextCancellation(t *testing.T) {
	cfg := &config.Config{
		WorkspaceRoot: "/workspaces",
		ToolsRoot:     "/tools",
	}

	tool := config.Tool{
		Cmd:  os.Args[0],
		Args: []string{"sleep"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := NativeRuntime{}
	cmd, _, stdout, stderr, err := rt.Spawn(ctx, cfg, tool)
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}

	// Garante que o processo iniciou.
	time.Sleep(50 * time.Millisecond)

	// Cancela o contexto -> CommandContext deve matar o processo.
	cancel()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// Esperamos que termine rápido.
		_ = stdout.Close()
		_ = stderr.Close()
		if err == nil {
			// Alguns OS retornam nil quando processo termina “limpo”, mas aqui deve ser raro.
			t.Log("process exited cleanly after cancel (ok)")
		}
	case <-time.After(2 * time.Second):
		// Se travou, é problema.
		_ = cmd.Process.Kill()
		t.Fatalf("process did not exit after context cancellation")
	}
}

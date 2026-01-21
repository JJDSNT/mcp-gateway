package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-router/internal/config"
)

func TestDockerRuntime_Spawn_BuildsExpectedDockerArgsAndPassesArgsLiterally(t *testing.T) {
	// Arrange: cria um "docker" fake no PATH.
	tmp := t.TempDir()
	fakeDockerPath := filepath.Join(tmp, "docker")

	fakeScript := `#!/bin/sh
# imprime args, um por linha (facilita comparar)
for a in "$@"; do
  echo "$a"
done
exit 0
`
	if err := os.WriteFile(fakeDockerPath, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}

	// PATH do processo de teste + env do Spawn
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

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
		Image: "alpine:latest",
		Args:  append([]string{"echoargs"}, dangerous...),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Act
	rt := DockerRuntime{}
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

	// Assert: prefix esperado
	wantPrefix := []string{
		"run", "-i", "--rm",
		"-v", fmt.Sprintf("%s:/workspaces", cfg.WorkspaceRoot),
		tool.Image,
		"echoargs",
	}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), string(outBytes))
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] mismatch: got %q want %q. full=%q", i, lines[i], want, string(outBytes))
		}
	}

	// Assert: dangerous args entram literalmente e na ordem
	gotDangerous := lines[len(wantPrefix):]
	if len(gotDangerous) != len(dangerous) {
		t.Fatalf("dangerous args count mismatch: got %d want %d. full=%q", len(gotDangerous), len(dangerous), string(outBytes))
	}
	for i, want := range dangerous {
		if gotDangerous[i] != want {
			t.Fatalf("dangerous arg[%d] mismatch: got %q want %q", i, gotDangerous[i], want)
		}
	}
}

func TestDockerRuntime_Spawn_SetsWorkspaceAndToolsEnv(t *testing.T) {
	tmp := t.TempDir()
	fakeDockerPath := filepath.Join(tmp, "docker")

	// fake docker imprime envs importantes e sai
	fakeScript := `#!/bin/sh
echo "$WORKSPACE_ROOT"
echo "$TOOLS_ROOT"
exit 0
`
	if err := os.WriteFile(fakeDockerPath, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	cfg := &config.Config{
		WorkspaceRoot: "/workspaces",
		ToolsRoot:     "/tools",
	}
	tool := config.Tool{
		Image: "alpine:latest",
		Args:  []string{"printenv"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rt := DockerRuntime{}
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

func TestDockerRuntime_Spawn_RespectsContextCancellation(t *testing.T) {
	tmp := t.TempDir()
	fakeDockerPath := filepath.Join(tmp, "docker")

	// fake docker fica rodando até ser morto
	fakeScript := `#!/bin/sh
while true; do
  sleep 0.2
done
`
	if err := os.WriteFile(fakeDockerPath, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	cfg := &config.Config{
		WorkspaceRoot: "/workspaces",
		ToolsRoot:     "/tools",
	}
	tool := config.Tool{
		Image: "alpine:latest",
		Args:  []string{"sleep"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := DockerRuntime{}
	cmd, _, _, _, err := rt.Spawn(ctx, cfg, tool)
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}

	// garante que iniciou
	time.Sleep(50 * time.Millisecond)

	cancel()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// ok: terminou após cancel
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("process did not exit after context cancellation")
	}
}

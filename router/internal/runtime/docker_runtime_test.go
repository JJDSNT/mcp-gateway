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

	// Hardening defaults: network=none, read_only=true
	tool := config.Tool{
		Runtime: "container",
		Image:   "alpine:latest",
		Args:    append([]string{"echoargs"}, dangerous...),
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

	// Assert: começa com run -i --rm
	wantStart := []string{"run", "-i", "--rm"}
	if len(lines) < len(wantStart) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantStart), len(lines), string(outBytes))
	}
	for i, want := range wantStart {
		if lines[i] != want {
			t.Fatalf("arg[%d] mismatch: got %q want %q. full=%q", i, lines[i], want, string(outBytes))
		}
	}

	// Assert: hardening flags presentes (ordem pode variar ao longo do tempo; testamos por presença)
	mustContain := [][]string{
		{"--security-opt=no-new-privileges"},
		{"--cap-drop=ALL"},
		{"--network", "none"},
		{"--read-only"},
		{"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m"},
		{"--tmpfs", "/var/tmp:rw,noexec,nosuid,size=64m"},
		{"-v", fmt.Sprintf("%s:/workspaces", cfg.WorkspaceRoot)},
	}
	for _, seq := range mustContain {
		if !containsSubsequence(lines, seq) {
			t.Fatalf("missing subsequence %v. full=%q", seq, string(outBytes))
		}
	}

	// Assert: imagem aparece e args da tool vêm depois dela (literalmente e na ordem)
	imgIdx := indexOf(lines, tool.Image)
	if imgIdx == -1 {
		t.Fatalf("expected image %q in args. full=%q", tool.Image, string(outBytes))
	}

	// Tudo depois da imagem deve ser exatamente tool.Args
	gotAfterImage := lines[imgIdx+1:]
	if len(gotAfterImage) != len(tool.Args) {
		t.Fatalf("tool args count mismatch: got %d want %d. afterImage=%v full=%q",
			len(gotAfterImage), len(tool.Args), gotAfterImage, string(outBytes))
	}
	for i, want := range tool.Args {
		if gotAfterImage[i] != want {
			t.Fatalf("tool arg[%d] mismatch: got %q want %q. full=%q", i, gotAfterImage[i], want, string(outBytes))
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
		Runtime: "container",
		Image:   "alpine:latest",
		Args:    []string{"printenv"},
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
		Runtime: "container",
		Image:   "alpine:latest",
		Args:    []string{"sleep"},
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

// --- helpers ---

func indexOf(xs []string, target string) int {
	for i, x := range xs {
		if x == target {
			return i
		}
	}
	return -1
}

// containsSubsequence verifica se seq aparece contiguamente em xs.
// Útil para checar presença de flags (--network none, -v <mount>, etc).
func containsSubsequence(xs []string, seq []string) bool {
	if len(seq) == 0 {
		return true
	}
	for i := 0; i <= len(xs)-len(seq); i++ {
		ok := true
		for j := 0; j < len(seq); j++ {
			if xs[i+j] != seq[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

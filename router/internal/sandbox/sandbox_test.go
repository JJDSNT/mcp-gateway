package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateToolName_Valid(t *testing.T) {
	tests := []string{
		"echo",
		"filesystem",
		"my-tool",
		"my_tool",
		"tool123",
		"Tool",
		"TOOL",
		"a",
		"Z9",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateToolName(name); err != nil {
				t.Errorf("ValidateToolName(%q) = %v, want nil", name, err)
			}
		})
	}
}

func TestValidateToolName_Invalid(t *testing.T) {
	tests := map[string]string{
		"":                "empty",
		"tool/name":       "slash",
		"tool\\name":      "backslash",
		"tool name":       "space",
		"tool\tname":      "tab",
		"tool\nname":      "newline",
		"../tool":         "parent dir",
		"tool/..":         "parent dir end",
		"tool%2fname":     "encoded slash %2f",
		"tool%2Fname":     "encoded slash %2F",
		"tool%5cname":     "encoded backslash %5c",
		"tool%5Cname":     "encoded backslash %5C",
		"tool%252fname":   "double-encoded slash",
		"tool@name":       "special char @",
		"tool#name":       "special char #",
		"tool$name":       "special char $",
	}

	for name, desc := range tests {
		t.Run(desc, func(t *testing.T) {
			err := ValidateToolName(name)
			if err == nil {
				t.Errorf("ValidateToolName(%q) = nil, want error", name)
			}
		})
	}
}

func TestValidatePath_Valid(t *testing.T) {
	tmpdir := t.TempDir()

	// Criar alguns arquivos/dirs para teste
	subdir := filepath.Join(tmpdir, "sub")
	os.Mkdir(subdir, 0755)

	file := filepath.Join(subdir, "test.txt")
	os.WriteFile(file, []byte("test"), 0644)

	tests := map[string]string{
		".":            "current dir",
		"sub":          "subdir",
		"sub/test.txt": "file in subdir",
		"./test.txt":   "dot slash file",
	}

	for path, desc := range tests {
		t.Run(desc, func(t *testing.T) {
			resolved, err := ValidatePath(tmpdir, path)
			if err != nil {
				t.Errorf("ValidatePath(%q) = error: %v, want resolved path", path, err)
			}
			// Garantir que resolved está dentro de tmpdir
			if !isPathInWorkspace(tmpdir, resolved) {
				t.Errorf("resolved path %q is outside workspace %q", resolved, tmpdir)
			}
		})
	}
}

func TestValidatePath_PathTraversal(t *testing.T) {
	tmpdir := t.TempDir()

	tests := map[string]string{
		"../etc":         "parent dir slash",
		"..%2fetc":       "encoded parent dir %2f",
		"..%252fetc":     "double-encoded parent dir",
		"..\\etc":        "backslash parent dir",
		"//etc/passwd":   "double slash",
		"/.":             "slash dot",
		"/etc/passwd":    "absolute path",
		"%2e%2e%2fetc":   "encoded ../",
		"%252e%252e%252fetc": "double-encoded ../",
	}

	for path, desc := range tests {
		t.Run(desc, func(t *testing.T) {
			_, err := ValidatePath(tmpdir, path)
			if err == nil {
				t.Errorf("ValidatePath(%q) = nil, want error for %s", path, desc)
			}
		})
	}
}

func TestValidatePath_SymlinkEscape(t *testing.T) {
	tmpdir := t.TempDir()

	// Criar um symlink que aponta para fora do workspace
	symlinkPath := filepath.Join(tmpdir, "escape_link")
	targetPath := "/"

	// Criar o symlink
	err := os.Symlink(targetPath, symlinkPath)
	if err != nil {
		// Se não conseguir criar symlink (ex: Windows sem permissões),
		// pular o teste
		t.Skipf("cannot create symlink: %v", err)
	}

	// Tentar acessar dentro do symlink
	_, err = ValidatePath(tmpdir, "escape_link/etc/passwd")
	if err == nil {
		t.Errorf("ValidatePath should reject symlink escape, got nil")
	}

	// Tentar acessar o próprio symlink
	_, err = ValidatePath(tmpdir, "escape_link")
	if err == nil {
		t.Errorf("ValidatePath should reject symlink to root, got nil")
	}
}

func TestValidatePath_SymlinkToRoot(t *testing.T) {
	tmpdir := t.TempDir()

	// Criar symlink -> /
	symlinkPath := filepath.Join(tmpdir, "root_link")
	err := os.Symlink("/", symlinkPath)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Acessar através do symlink
	_, err = ValidatePath(tmpdir, "root_link")
	if err == nil {
		t.Errorf("ValidatePath should reject symlink to /, got nil")
	}
}

func isPathInWorkspace(workspace, path string) bool {
	wsAbs := filepath.Clean(workspace)
	pathAbs := filepath.Clean(path)
	
	// Se for igual, está dentro
	if pathAbs == wsAbs {
		return true
	}
	
	// Se começar com workspace + separator, está dentro
	return len(pathAbs) > len(wsAbs) &&
		pathAbs[:len(wsAbs)] == wsAbs &&
		pathAbs[len(wsAbs)] == filepath.Separator
}

// Testes para URL encoding bypass attempts
func TestValidatePath_EncodingBypass(t *testing.T) {
	tmpdir := t.TempDir()

	// Variações de encoding para ../
	tests := []string{
		"..%2f",
		"..%2F",
		"%2e%2e%2f",
		"%2e%2e/",
		".%2e/",
		"/%2e%2e",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			_, err := ValidatePath(tmpdir, path)
			if err == nil {
				t.Errorf("ValidatePath(%q) should reject encoding bypass, got nil", path)
			}
		})
	}
}

// Teste para caminhos que parecem válidos mas contêm .. disfarçados
func TestValidatePath_DisguisedParentDir(t *testing.T) {
	tmpdir := t.TempDir()

	// /./ deveria ser rejeitado
	_, err := ValidatePath(tmpdir, "/./test")
	if err == nil {
		t.Errorf("ValidatePath should reject /./ pattern")
	}

	// /./ com encoding
	_, err = ValidatePath(tmpdir, "/.%2f")
	if err == nil {
		t.Errorf("ValidatePath should reject encoded /./ pattern")
	}
}

func TestValidatePath_DoesNotAllowPrefixBoundaryBypass(t *testing.T) {
	rootParent := t.TempDir()
	root := filepath.Join(rootParent, "ws")
	root2 := filepath.Join(rootParent, "ws2")

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.MkdirAll(root2, 0o755); err != nil {
		t.Fatalf("mkdir root2: %v", err)
	}

	// Tenta forçar um resolved path em ws2 e garantir que ws não aceita.
	// Aqui simulamos chamando ValidatePath(root, "../ws2/file") — mas isso é bloqueado por "..".
	// Então a verificação mais relevante é garantir que a checagem interna não use HasPrefix de forma insegura.
	// Vamos testar diretamente um caso que pode surgir após EvalSymlinks: root="/tmp/ws", evalPath="/tmp/ws2/file".
	// Como ValidatePath não recebe evalPath diretamente, validamos o comportamento via symlink:
	// cria ws/child -> ../ws2 (symlink relativo) e pede child/file.
	link := filepath.Join(root, "child")
	if err := os.Symlink("../ws2", link); err != nil {
		// Em Windows, symlink pode exigir permissões; se não der, skip.
		if runtime.GOOS == "windows" {
			t.Skip("symlink not permitted on this Windows environment")
		}
		t.Fatalf("symlink: %v", err)
	}

	// arquivo pode não existir; o importante é que resolve pra ws2
	_, err := ValidatePath(root, "child/file.txt")
	if err == nil {
		t.Fatalf("expected error, got nil (prefix boundary bypass risk)")
	}
}

func TestValidatePath_RejectsSymlinkEscapeEvenIfTargetDoesNotExist(t *testing.T) {
	root := t.TempDir()
	wsRoot := filepath.Join(root, "ws")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}

	// ws/link -> /etc (ou outra pasta existente)
	// Em ambientes sem /etc (Windows), usamos o próprio rootParent como "outside".
	outside := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		outside = root // "outside" será o próprio tempdir parent; ainda serve pra escape (aponta pra fora de wsRoot)
	}

	link := filepath.Join(wsRoot, "link")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink not permitted on this Windows environment")
		}
		t.Fatalf("symlink: %v", err)
	}

	// Pedimos um caminho que provavelmente NÃO existe para forçar o caminho do fallback se EvalSymlinks falhar.
	_, err := ValidatePath(wsRoot, "link/this-file-should-not-exist-12345")
	if err == nil {
		t.Fatalf("expected error (symlink escape must be rejected even if target does not exist)")
	}
	// opcional: ajuda a garantir que estamos rejeitando pelo motivo certo
	if !strings.Contains(err.Error(), "escapes workspace") && !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

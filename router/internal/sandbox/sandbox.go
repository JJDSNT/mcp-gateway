package sandbox

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ValidateToolName verifica se o nome da tool é válido
// Rejeita: /, .., %2f, \, espaços e outros caracteres suspeitos
func ValidateToolName(name string) error {
	if name == "" {
		return fmt.Errorf("tool name is empty")
	}

	// Sem espaços
	if strings.ContainsAny(name, " \t\n\r") {
		return fmt.Errorf("tool name contains whitespace")
	}

	// Sem path separators
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("tool name contains path separator")
	}

	// Sem parent directory references
	if strings.Contains(name, "..") {
		return fmt.Errorf("tool name contains parent directory reference")
	}

	// Sem encoded slashes
	if strings.Contains(name, "%2f") || strings.Contains(name, "%2F") ||
		strings.Contains(name, "%5c") || strings.Contains(name, "%5C") { // %5c = \
		return fmt.Errorf("tool name contains encoded path separator")
	}

	// Sem double encoded
	if strings.Contains(name, "%25") {
		return fmt.Errorf("tool name contains double-encoded characters")
	}

	// Alfanumérico + traço/underscore
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_') {
			return fmt.Errorf("tool name contains invalid character: %c", ch)
		}
	}

	return nil
}

// ValidatePath verifica se um caminho está dentro do workspace root
// Bloqueia: ../, encoding, symlinks que escapam, etc.
func ValidatePath(workspaceRoot, requestedPath string) (string, error) {
	// Normalizar workspace root
	wsRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("invalid workspace root: %w", err)
	}

	// Verificar se workspace root existe
	if _, err := os.Stat(wsRoot); err != nil {
		return "", fmt.Errorf("workspace root not found: %w", err)
	}

	// Rejeitar path traversal padrão
	if err := checkPathTraversal(requestedPath); err != nil {
		return "", err
	}

	// Decodificar URL encoding uma vez
	decoded, err := url.QueryUnescape(requestedPath)
	if err != nil {
		return "", fmt.Errorf("invalid URL encoding: %w", err)
	}

	// Checar novamente após decodificar
	if err := checkPathTraversal(decoded); err != nil {
		return "", fmt.Errorf("path traversal after decoding: %w", err)
	}

	// Decodificar novamente (double encoding como %252e%252e%252f)
	doubleDecoded, err := url.QueryUnescape(decoded)
	if err == nil && doubleDecoded != decoded {
		// Se mudou após segunda decodificação, checar novamente
		if err := checkPathTraversal(doubleDecoded); err != nil {
			return "", fmt.Errorf("path traversal after double decoding: %w", err)
		}
	}

	// Validar componente por componente: cada symlink deve estar dentro do workspace
	wsRoot = filepath.Clean(wsRoot)
	pathParts := strings.Split(decoded, string(filepath.Separator))
	currentPath := wsRoot

	for i, part := range pathParts {
		if part == "" || part == "." {
			continue
		}

		currentPath = filepath.Join(currentPath, part)

		// Checar se o componente é um symlink
		linkTarget, err := os.Readlink(currentPath)
		if err == nil {
			// É um symlink - precisamos validar onde ele aponta
			if filepath.IsAbs(linkTarget) {
				// Symlink absoluto - sempre é escape
				return "", fmt.Errorf("symlink escapes workspace: absolute symlink %s", part)
			} else {
				// Symlink relativo - resolver da perspectiva do diretório pai
				linkDir := filepath.Dir(currentPath)
				resolvedLink := filepath.Join(linkDir, linkTarget)
				resolvedLink = filepath.Clean(resolvedLink)

				// Verificar se o symlink resolvido está dentro do workspace
				// Usar separator check para evitar false positives (ex: /ws e /ws2)
				inWorkspace := resolvedLink == wsRoot || (strings.HasPrefix(resolvedLink, wsRoot+string(filepath.Separator)))
				if !inWorkspace {
					return "", fmt.Errorf("symlink escapes workspace: %s -> %s", part, resolvedLink)
				}

				// Validar cadeias de symlinks: se o target é também um symlink, validar recursivamente
				// Usar EvalSymlinks no target para detectar cadeias
				evaledTarget, errEval := filepath.EvalSymlinks(resolvedLink)
				if errEval == nil {
					evaledTarget = filepath.Clean(evaledTarget)
					inWorkspace := evaledTarget == wsRoot || (strings.HasPrefix(evaledTarget, wsRoot+string(filepath.Separator)))
					if !inWorkspace {
						return "", fmt.Errorf("symlink chain escapes workspace: %s resolves to %s", part, evaledTarget)
					}
				}

				// Se é um symlink que escapa, precisamos checar os componentes restantes
				// para garantir que não há path traversal através do symlink
				remainingParts := pathParts[i+1:]
				for _, remainingPart := range remainingParts {
					if remainingPart == "" || remainingPart == "." {
						continue
					}
					// Tentar recompor o caminho através do symlink para verificar
					currentPath = filepath.Join(currentPath, remainingPart)
				}
				// Checar novamente o caminho final
				currentPath = filepath.Clean(currentPath)
				inWorkspace = currentPath == wsRoot || (strings.HasPrefix(currentPath, wsRoot+string(filepath.Separator)))
				if !inWorkspace {
					return "", fmt.Errorf("symlink escapes workspace after resolution")
				}
			}
		}
		// Se não é symlink, continua normalmente
	}

	// Agora resolver o caminho final
	fullPath := filepath.Join(wsRoot, decoded)
	evalPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		// Se não conseguir avaliar symlinks (ex: arquivo não existe),
		// usar o caminho joined normalizado
		evalPath = filepath.Clean(fullPath)
	}

	// Garantir que o caminho resolvido está dentro de workspace root
	evalPath = filepath.Clean(evalPath)
	wsRoot = filepath.Clean(wsRoot)

	// Usar separator check para evitar false positives (ex: /ws e /ws2)
	inWorkspace := evalPath == wsRoot || (strings.HasPrefix(evalPath, wsRoot+string(filepath.Separator)))
	if !inWorkspace {
		return "", fmt.Errorf("path escapes workspace: %s not in %s", evalPath, wsRoot)
	}

	return evalPath, nil
}

// checkPathTraversal verifica variantes comuns de path traversal
func checkPathTraversal(path string) error {
	// Rejeitar path que começa com /
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("path cannot be absolute")
	}

	// Rejeitar ... e ../ e ..\
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal detected: contains ..")
	}

	// Rejeitar // (path resolution confusion)
	if strings.Contains(path, "//") {
		return fmt.Errorf("path traversal detected: contains //")
	}

	// Rejeitar /. (confusão)
	if strings.Contains(path, "/.") {
		return fmt.Errorf("path traversal detected: contains /.")
	}

	return nil
}

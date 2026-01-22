package logging

import "log/slog"

// Campos fixos do projeto.
// Estes helpers garantem consistência entre transport, core, runtime e shims.

// Tool identifica a ferramenta MCP sendo executada.
func Tool(name string) slog.Attr {
	return slog.String("tool", name)
}

// Runtime identifica o runtime da tool (native, container, etc).
func Runtime(name string) slog.Attr {
	return slog.String("runtime", name)
}

// RequestID identifica unicamente um request ponta-a-ponta
// (cliente → gateway → runtime → shim).
func RequestID(id string) slog.Attr {
	return slog.String("request_id", id)
}

// DurationMs representa duração em milissegundos.
// Use sempre duration_ms (não misturar com duration_ns/s).
func DurationMs(ms int64) slog.Attr {
	return slog.Int64("duration_ms", ms)
}

// Err normaliza erros em logs.
// Sempre logado como string (não como objeto Go).
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Any("error", nil)
	}
	return slog.String("error", err.Error())
}

// ---- Helpers opcionais (não obrigatórios, mas úteis) ----

// Bool adiciona um boolean padronizado.
func Bool(key string, v bool) slog.Attr {
	return slog.Bool(key, v)
}

// Int adiciona um inteiro padronizado.
func Int(key string, v int) slog.Attr {
	return slog.Int(key, v)
}

// Int64 adiciona um int64 padronizado.
func Int64(key string, v int64) slog.Attr {
	return slog.Int64(key, v)
}

// String adiciona uma string padronizada.
func String(key, v string) slog.Attr {
	return slog.String(key, v)
}

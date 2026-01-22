package shim

import "log/slog"

// Campos alinhados com o projeto (tool/runtime/request_id/duration/error)
// Mesmo que o shim não use todos sempre, manter padrão ajuda a correlacionar logs.

func Tool(name string) slog.Attr {
	return slog.String("tool", name)
}

func Runtime(name string) slog.Attr {
	return slog.String("runtime", name)
}

func RequestID(id string) slog.Attr {
	return slog.String("request_id", id)
}

func DurationMs(ms int64) slog.Attr {
	return slog.Int64("duration_ms", ms)
}

func Err(err error) slog.Attr {
	if err == nil {
		return slog.Any("error", nil)
	}
	return slog.String("error", err.Error())
}

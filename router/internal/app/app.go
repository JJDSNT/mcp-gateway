package app

import (
	"context"

	"mcp-router/internal/config"
	"mcp-router/internal/core"
	"mcp-router/internal/transport"
)

type App struct {
	core  *core.Service
	http  *transport.HTTP
	stdio *transport.Stdio
}

func New(configPath string) (*App, error) {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return nil, err
	}

	svc := core.New(cfg)

	return &App{
		core:  svc,
		http:  transport.NewHTTP(svc),
		stdio: transport.NewStdio(svc),
	}, nil
}

func (a *App) RunStdio(ctx context.Context) error {
	return a.stdio.Run(ctx)
}

func (a *App) RunHTTP(ctx context.Context, addr string) error {
	return a.http.Run(ctx, addr)
}

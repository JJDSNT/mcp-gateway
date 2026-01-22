package app

import (
	"context"
	"fmt"
	"log"

	"mcp-router/internal/config"
	"mcp-router/internal/core"
	"mcp-router/internal/transport"
)

type App struct {
	http  *transport.HTTP
	stdio *transport.Stdio
}

func New(configPath string) (*App, error) {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	svc := core.New(cfg)

	// opcional: log centralizado aqui
	log.Println("Loaded tools:")
	for k := range cfg.Tools {
		log.Println(" -", k)
	}

	return &App{
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

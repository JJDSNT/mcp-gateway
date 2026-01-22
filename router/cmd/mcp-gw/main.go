package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"mcp-router/internal/app"
)

func main() {
	defaultConfig := pickDefaultConfig()

	var (
		configPath = flag.String("config", defaultConfig, "path to config.yaml")
		httpAddr   = flag.String("http", "", "if set, start HTTP server on this address (e.g. :8080)")
		alsoStdio  = flag.Bool("stdio", false, "if true, also run stdio even when --http is set")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	// default: stdio (MCP)
	if *httpAddr == "" {
		log.Printf("mcp-gw starting (stdio) config=%s", *configPath)
		if err := a.RunStdio(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	// http mode
	log.Printf("mcp-gw starting (http) addr=%s config=%s", *httpAddr, *configPath)

	if *alsoStdio {
		log.Printf("mcp-gw also running stdio")
		go func() {
			if err := a.RunStdio(ctx); err != nil {
				log.Printf("stdio error: %v", err)
				stop()
			}
		}()
	}

	if err := a.RunHTTP(ctx, *httpAddr); err != nil {
		log.Fatal(err)
	}
}

// pickDefaultConfig tenta facilitar a vida:
// - se existir ./config/config.yaml (rodando da raiz do repo), usa
// - senão, usa /config/config.yaml (Docker)
func pickDefaultConfig() string {
	candidate := filepath.Join(".", "config", "config.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return "/config/config.yaml"
}

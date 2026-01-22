package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"mcp-router/internal/app"
)

func main() {
	var (
		configPath = flag.String("config", "/config/config.yaml", "path to config.yaml")
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

	// default: stdio
	if *httpAddr == "" {
		if err := a.RunStdio(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	// http mode
	if *alsoStdio {
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

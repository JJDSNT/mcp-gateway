SHELL := /bin/bash

# ----------------------------
# Paths
# ----------------------------
ROUTER_DIR := router
DIST_DIR := dist
CERTS_DIR := certs

# ----------------------------
# Binary
# ----------------------------
BIN := mcp-gw

# ----------------------------
# Docker
# ----------------------------
COMPOSE := docker compose
CADDY_CONTAINER := mcp-caddy
CADDY_ROOT_CA_PATH := /data/caddy/pki/authorities/local/root.crt
CADDY_ROOT_CA_OUT := $(CERTS_DIR)/caddy-local-root.crt

# ----------------------------
# Go build
# ----------------------------
GO ?= go
GO_LINUX_ENV := GOOS=linux GOARCH=amd64
GO_WINDOWS_ENV := GOOS=windows GOARCH=amd64

.PHONY: help \
        up down rebuild ps logs tunnel-up tunnel-down \
        cert-export cert-export-check cert-install-wsl \
        build build-linux build-windows clean \
        install-linux uninstall-linux \
        test fmt tidy

# ----------------------------
# Help
# ----------------------------
help:
	@echo "Targets:"
	@echo ""
	@echo "Docker:"
	@echo "  up                 - docker compose up -d --build"
	@echo "  down               - docker compose down"
	@echo "  rebuild            - rebuild and restart"
	@echo "  ps                 - docker compose ps"
	@echo "  logs               - docker compose logs -f"
	@echo "  tunnel-up          - start with cloudflared profile"
	@echo "  tunnel-down        - stop tunnel profile"
	@echo ""
	@echo "Certificates (Caddy tls internal):"
	@echo "  cert-export        - export Caddy root CA to ./certs/"
	@echo "  cert-install-wsl   - install exported root CA into Linux/WSL trust store"
	@echo ""
	@echo "Build:"
	@echo "  build              - build linux + windows binaries into ./dist/"
	@echo "  build-linux        - build linux binary"
	@echo "  build-windows      - build windows binary"
	@echo "  install-linux      - install linux binary to /usr/local/bin (sudo)"
	@echo ""
	@echo "Dev:"
	@echo "  test               - go test ./... (router)"
	@echo "  fmt                - gofmt"
	@echo "  tidy               - go mod tidy"
	@echo "  clean              - remove dist/ and certs/"

# ----------------------------
# Docker lifecycle
# ----------------------------
up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

rebuild:
	$(COMPOSE) down
	$(COMPOSE) up -d --build

ps:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs -f

tunnel-up:
	$(COMPOSE) --profile tunnel up -d --build

tunnel-down:
	$(COMPOSE) --profile tunnel down

# ----------------------------
# Cert workflow (Caddy tls internal)
# ----------------------------
cert-export: cert-export-check
	@mkdir -p $(CERTS_DIR)
	@echo "Exporting Caddy root CA from container '$(CADDY_CONTAINER)'..."
	docker cp $(CADDY_CONTAINER):$(CADDY_ROOT_CA_PATH) $(CADDY_ROOT_CA_OUT)
	@echo "Saved: $(CADDY_ROOT_CA_OUT)"
	@echo ""
	@echo "Next steps:"
	@echo "- Windows: import this .crt into 'Trusted Root Certification Authorities' (Local Machine)."
	@echo "- WSL/Linux: make cert-install-wsl"

cert-export-check:
	@echo "Checking root CA exists inside container..."
	docker exec -i $(CADDY_CONTAINER) sh -lc 'test -f "$(CADDY_ROOT_CA_PATH)" && echo "OK: $(CADDY_ROOT_CA_PATH)" || (echo "ERROR: not found $(CADDY_ROOT_CA_PATH)"; exit 1)'

cert-install-wsl:
	@if [ ! -f "$(CADDY_ROOT_CA_OUT)" ]; then \
		echo "ERROR: cert not found at $(CADDY_ROOT_CA_OUT). Run: make cert-export"; \
		exit 1; \
	fi
	@echo "Installing root CA into Linux/WSL trust store (requires sudo)..."
	sudo cp "$(CADDY_ROOT_CA_OUT)" /usr/local/share/ca-certificates/caddy-local-root.crt
	sudo update-ca-certificates
	@echo "Done. Try: curl -i https://localhost"

# ----------------------------
# Go build
# ----------------------------
build: build-linux build-windows

build-linux:
	@mkdir -p $(DIST_DIR)
	@echo "Building $(BIN) for linux/amd64..."
	cd $(ROUTER_DIR) && $(GO_LINUX_ENV) $(GO) build -o ../$(DIST_DIR)/$(BIN) .

build-windows:
	@mkdir -p $(DIST_DIR)
	@echo "Building $(BIN).exe for windows/amd64..."
	cd $(ROUTER_DIR) && $(GO_WINDOWS_ENV) $(GO) build -o ../$(DIST_DIR)/$(BIN).exe .

install-linux: build-linux
	@echo "Installing to /usr/local/bin/$(BIN) (requires sudo)..."
	sudo cp "$(DIST_DIR)/$(BIN)" /usr/local/bin/$(BIN)
	sudo chmod +x /usr/local/bin/$(BIN)
	@echo "Installed. Verify: which $(BIN) && $(BIN) --help"

uninstall-linux:
	@echo "Removing /usr/local/bin/$(BIN) (requires sudo)..."
	sudo rm -f /usr/local/bin/$(BIN)
	@echo "Removed."

# ----------------------------
# Go dev helpers
# ----------------------------
test:
	cd $(ROUTER_DIR) && $(GO) test ./...

fmt:
	cd $(ROUTER_DIR) && gofmt -w .

tidy:
	cd $(ROUTER_DIR) && $(GO) mod tidy

clean:
	rm -rf $(DIST_DIR) $(CERTS_DIR)

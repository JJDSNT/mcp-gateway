SHELL := /bin/bash

# ----------------------------
# Paths
# ----------------------------
ROUTER_DIR := router
DIST_DIR := dist
CERTS_DIR := certs

# ----------------------------
# Certificates (RSA)
# ----------------------------
RSA_ROOT_KEY := $(CERTS_DIR)/root.key
RSA_ROOT_CRT := $(CERTS_DIR)/root.crt
RSA_INT_KEY  := $(CERTS_DIR)/intermediate.key
RSA_INT_CRT  := $(CERTS_DIR)/intermediate.crt
RSA_INT_CSR  := $(CERTS_DIR)/intermediate.csr
RSA_INT_EXT  := $(CERTS_DIR)/intermediate.ext

# ----------------------------
# Binaries
# ----------------------------
BIN := mcp-gw
SHIM_PROC := mcp-gw-shim-proc.exe
SHIM_XPORT := mcp-gw-shim-xport.exe

# ----------------------------
# Docker
# ----------------------------
COMPOSE := docker compose

# ----------------------------
# Go build
# ----------------------------
GO ?= go
GO_LINUX_ENV := GOOS=linux GOARCH=amd64
GO_WINDOWS_ENV := GOOS=windows GOARCH=amd64

# Entry points
GW_PKG := ./cmd/mcp-gw
SHIM_PROC_PKG := ./cmd/mcp-gw-shim-proc
SHIM_XPORT_PKG := ./cmd/mcp-gw-shim-xport

# ----------------------------
# Version metadata
# ----------------------------
VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT   := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo unknown)

LDFLAGS := -s -w \
	-X 'mcp-router/internal/cli.Version=$(VERSION)' \
	-X 'mcp-router/internal/cli.Commit=$(COMMIT)' \
	-X 'mcp-router/internal/cli.BuildDate=$(BUILT)'

# ----------------------------
# Phony targets
# ----------------------------
.PHONY: help up down rebuild ps logs \
        rsa-gen rsa-test rsa-install-wsl \
        build build-all build-linux build-windows \
        verify test fmt tidy clean clean-certs

# ----------------------------
# Help
# ----------------------------
help:
	@echo ""
	@echo "Docker:"
	@echo "  up                - docker compose up -d"
	@echo "  rebuild           - docker compose up -d --build"
	@echo "  down              - docker compose down"
	@echo "  ps                - docker compose ps"
	@echo "  logs              - docker compose logs -f"
	@echo ""
	@echo "Certificates:"
	@echo "  rsa-gen           - generate RSA root + intermediate"
	@echo "  rsa-test          - inspect generated certs"
	@echo "  rsa-install-wsl   - trust RSA root in WSL/Linux"
	@echo ""
	@echo "Build:"
	@echo "  build             - build linux (safe default)"
	@echo "  build-all         - build linux + windows"
	@echo "  build-linux       - build linux binary"
	@echo "  build-windows     - build windows binaries"
	@echo ""
	@echo "Dev:"
	@echo "  verify            - test + build-linux"
	@echo "  test              - go test"
	@echo "  fmt               - gofmt"
	@echo "  tidy              - go mod tidy"
	@echo "  clean             - remove build artifacts"
	@echo "  clean-certs       - remove local certificates (DANGEROUS)"

# ----------------------------
# Docker lifecycle
# ----------------------------
up:
	$(COMPOSE) up -d

rebuild:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

ps:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs -f

# ----------------------------
# RSA PKI generation
# ----------------------------
rsa-gen:
	@mkdir -p $(CERTS_DIR)
	@if [ -f "$(RSA_ROOT_CRT)" ]; then \
		echo "RSA root certificate already exists."; \
		echo "Using existing certs in ./certs/"; \
		exit 0; \
	fi
	@echo "Generating RSA Root CA..."
	@openssl genrsa -out "$(RSA_ROOT_KEY)" 2048
	@openssl req -x509 -new -nodes -key "$(RSA_ROOT_KEY)" -sha256 -days 3650 \
		-subj "/CN=MCP Local RSA Root" \
		-out "$(RSA_ROOT_CRT)"
	@echo "Generating RSA Intermediate CA..."
	@openssl genrsa -out "$(RSA_INT_KEY)" 2048
	@openssl req -new -key "$(RSA_INT_KEY)" \
		-subj "/CN=MCP Local RSA Intermediate" \
		-out "$(RSA_INT_CSR)"
	@printf "basicConstraints=CA:TRUE,pathlen:0\nkeyUsage=keyCertSign,cRLSign\n" > "$(RSA_INT_EXT)"
	@openssl x509 -req -in "$(RSA_INT_CSR)" \
		-CA "$(RSA_ROOT_CRT)" -CAkey "$(RSA_ROOT_KEY)" -CAcreateserial \
		-out "$(RSA_INT_CRT)" -days 3650 -sha256 -extfile "$(RSA_INT_EXT)"
	@rm -f "$(RSA_INT_CSR)" "$(RSA_INT_EXT)" "$(CERTS_DIR)/root.srl"
	@echo "OK: certificates generated in ./certs/"

rsa-test:
	@openssl x509 -in "$(RSA_ROOT_CRT)" -noout -subject -issuer -dates
	@openssl x509 -in "$(RSA_INT_CRT)" -noout -subject -issuer -dates

rsa-install-wsl:
	sudo cp "$(RSA_ROOT_CRT)" /usr/local/share/ca-certificates/mcp-gw-local-root.crt
	sudo update-ca-certificates

# ----------------------------
# Go build
# ----------------------------
build: build-linux

build-all: build-linux build-windows

build-linux:
	@mkdir -p $(DIST_DIR)
	@echo "Building $(BIN) for linux/amd64..."
	cd $(ROUTER_DIR) && $(GO_LINUX_ENV) $(GO) build \
		-ldflags "$(LDFLAGS)" \
		-o ../$(DIST_DIR)/$(BIN) \
		$(GW_PKG)

build-windows:
	@mkdir -p $(DIST_DIR)
	@echo "Building Windows binaries..."
	cd $(ROUTER_DIR) && $(GO_WINDOWS_ENV) $(GO) build \
		-ldflags "$(LDFLAGS)" \
		-o ../$(DIST_DIR)/$(BIN).exe \
		$(GW_PKG)
	cd $(ROUTER_DIR) && $(GO_WINDOWS_ENV) $(GO) build \
		-o ../$(DIST_DIR)/$(SHIM_PROC) \
		$(SHIM_PROC_PKG)
	cd $(ROUTER_DIR) && $(GO_WINDOWS_ENV) $(GO) build \
		-o ../$(DIST_DIR)/$(SHIM_XPORT) \
		$(SHIM_XPORT_PKG)

# ----------------------------
# Go helpers
# ----------------------------
verify: test build-linux
	@echo "OK: test + build-linux passed"

test:
	cd $(ROUTER_DIR) && $(GO) test -count=1 ./...

fmt:
	cd $(ROUTER_DIR) && gofmt -w .

tidy:
	cd $(ROUTER_DIR) && $(GO) mod tidy

# ----------------------------
# Clean
# ----------------------------
clean:
	rm -rf $(DIST_DIR)
	@echo "Clean done (artifacts only)."

clean-certs:
	rm -rf $(CERTS_DIR)
	@echo "Certificates removed."
	@echo "Run 'make rsa-gen' and re-trust the root CA if needed."

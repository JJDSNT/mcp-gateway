SHELL := /bin/bash

# ----------------------------
# Paths
# ----------------------------
ROUTER_DIR := router
DIST_DIR := dist
CERTS_DIR := certs

# Flat certs/ layout
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

.PHONY: help up down rebuild ps logs \
        rsa-gen rsa-test rsa-install-wsl \
        build build-linux build-windows \
        verify test fmt tidy clean

# ----------------------------
# Help
# ----------------------------
help:
	@echo ""
	@echo "Docker:"
	@echo "  up                - docker compose up -d --build"
	@echo "  down              - docker compose down"
	@echo "  rebuild           - rebuild containers"
	@echo "  ps                - docker compose ps"
	@echo "  logs              - docker compose logs -f"
	@echo ""
	@echo "Certificates (RSA - default):"
	@echo "  rsa-gen           - generate RSA root + intermediate in ./certs/"
	@echo "  rsa-test          - inspect generated RSA certs"
	@echo "  rsa-install-wsl   - trust RSA root in WSL/Linux"
	@echo ""
	@echo "Build:"
	@echo "  build             - build linux + windows (incl. shims) into ./dist/"
	@echo "  build-linux       - build linux binary"
	@echo "  build-windows     - build windows gateway + shims"
	@echo ""
	@echo "Dev:"
	@echo "  verify            - test + build-linux"
	@echo "  test              - go test"
	@echo "  fmt               - gofmt"
	@echo "  tidy              - go mod tidy"
	@echo "  clean             - remove dist/ and generated certs"

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

# ----------------------------
# RSA PKI generation (idempotent)
# ----------------------------
rsa-gen:
	@mkdir -p $(CERTS_DIR)
	@if [ -f "$(RSA_ROOT_CRT)" ]; then \
		echo "RSA root certificate already exists."; \
		echo "Using existing certificates in ./certs/"; \
		echo ""; \
		echo "If you want to regenerate, remove certs/root.crt and run:"; \
		echo "  make rsa-gen"; \
		exit 0; \
	fi
	@echo "Generating RSA Root CA..."
	@openssl genrsa -out "$(RSA_ROOT_KEY)" 2048
	@openssl req -x509 -new -nodes -key "$(RSA_ROOT_KEY)" -sha256 -days 3650 \
		-subj "/CN=MCP Caddy Local Authority RSA Root" \
		-out "$(RSA_ROOT_CRT)"
	@echo "Generating RSA Intermediate CA..."
	@openssl genrsa -out "$(RSA_INT_KEY)" 2048
	@openssl req -new -key "$(RSA_INT_KEY)" \
		-subj "/CN=MCP Caddy Local Authority RSA Intermediate" \
		-out "$(RSA_INT_CSR)"
	@printf "basicConstraints=CA:TRUE,pathlen:0\nkeyUsage=keyCertSign,cRLSign\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\n" > "$(RSA_INT_EXT)"
	@openssl x509 -req -in "$(RSA_INT_CSR)" \
		-CA "$(RSA_ROOT_CRT)" -CAkey "$(RSA_ROOT_KEY)" -CAcreateserial \
		-out "$(RSA_INT_CRT)" -days 3650 -sha256 -extfile "$(RSA_INT_EXT)"
	@rm -f "$(RSA_INT_CSR)" "$(RSA_INT_EXT)" "$(CERTS_DIR)/root.srl"
	@echo ""
	@echo "OK: RSA PKI generated in ./certs/"
	@echo " - root.crt (install THIS on Windows)"
	@echo " - root.key (do NOT share/commit)"
	@echo " - intermediate.crt"
	@echo " - intermediate.key (do NOT share/commit)"
	@echo ""
	@echo "NEXT STEPS (once per machine):"
	@echo "1) Windows (PowerShell as Admin):"
	@echo "   certutil -addstore -f \"Root\" \"<PATH_TO_REPO>\\certs\\root.crt\""
	@echo "2) (Optional) WSL/Linux trust store:"
	@echo "   make rsa-install-wsl"
	@echo "3) Start stack:"
	@echo "   make up"
	@echo "4) Test:"
	@echo "   curl https://localhost"

rsa-test:
	@echo "Root:"
	@openssl x509 -in "$(RSA_ROOT_CRT)" -noout -subject -issuer -dates
	@openssl x509 -in "$(RSA_ROOT_CRT)" -noout -text | grep -E "Public Key Algorithm|Signature Algorithm"
	@echo ""
	@echo "Intermediate:"
	@openssl x509 -in "$(RSA_INT_CRT)" -noout -subject -issuer -dates
	@openssl x509 -in "$(RSA_INT_CRT)" -noout -text | grep -E "Public Key Algorithm|Signature Algorithm"

rsa-install-wsl:
	@if [ ! -f "$(RSA_ROOT_CRT)" ]; then \
		echo "ERROR: $(RSA_ROOT_CRT) not found. Run: make rsa-gen"; \
		exit 1; \
	fi
	@echo "Installing RSA root CA into Linux/WSL trust store (requires sudo)..."
	sudo cp "$(RSA_ROOT_CRT)" /usr/local/share/ca-certificates/mcp-gw-local-root.crt
	sudo update-ca-certificates
	@echo "Done. Try: curl https://localhost"

# ----------------------------
# Go build
# ----------------------------
build: build-linux build-windows

build-linux:
	@mkdir -p $(DIST_DIR)
	@echo "Building $(BIN) for linux/amd64..."
	cd $(ROUTER_DIR) && $(GO_LINUX_ENV) $(GO) build -o ../$(DIST_DIR)/$(BIN)

build-windows:
	@mkdir -p $(DIST_DIR)
	@echo "Building Windows binaries (gateway + shims) for windows/amd64..."
	cd $(ROUTER_DIR) && $(GO_WINDOWS_ENV) $(GO) build -o ../$(DIST_DIR)/$(BIN).exe .
	cd $(ROUTER_DIR) && $(GO_WINDOWS_ENV) $(GO) build -o ../$(DIST_DIR)/$(SHIM_PROC) ./cmd/mcp-gw-shim-proc
	cd $(ROUTER_DIR) && $(GO_WINDOWS_ENV) $(GO) build -o ../$(DIST_DIR)/$(SHIM_XPORT) ./cmd/mcp-gw-shim-xport

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

clean:
	rm -rf $(DIST_DIR)
	rm -f "$(RSA_ROOT_KEY)" "$(RSA_ROOT_CRT)" "$(RSA_INT_KEY)" "$(RSA_INT_CRT)"
	rm -f "$(CERTS_DIR)/root.srl"
	@echo "Clean done. certs/.keep preserved."

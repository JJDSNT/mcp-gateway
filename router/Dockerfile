# router/Dockerfile
# Multi-stage build: compile Go binary, then run in a slim image with runtimes + docker CLI

# --- build stage ---
FROM golang:1.22-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mcp-gw ./cmd/mcp-gw/main.go

# --- runtime stage ---
FROM debian:bookworm-slim

# Minimal deps:
# - ca-certificates: TLS
# - python3: for native tools like tools/echo.py
# - nodejs + npm: for native tools like "npx @modelcontextprotocol/server-git"
# - docker.io: provides docker CLI (used by Container Runtime / docker run)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    python3 \
    nodejs \
    npm \
    docker.io \
  && rm -rf /var/lib/apt/lists/*

# Create mount points (bind them via docker-compose)
RUN mkdir -p /config /tools /workspaces

COPY --from=build /out/mcp-gw /usr/local/bin/mcp-gw

EXPOSE 8080

# mcp-router loads /config/config.yaml (mounted by compose)
ENTRYPOINT ["/usr/local/bin/mcp-gw"]
CMD ["--http", ":8080"]

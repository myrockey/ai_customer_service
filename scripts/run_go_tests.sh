#!/bin/bash
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT/go_service"
docker run --rm -v "$PWD":/app -w /app -e GOPROXY=https://goproxy.cn,direct golang:1.25-alpine go test ./internal/pkg/auth/ -v 2>&1 | tail -40

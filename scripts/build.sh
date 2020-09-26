#!/bin/bash
BINARY=gitjacker
TAG=${TRAVIS_TAG:-development}
mkdir -p bin/darwin
GOOS=darwin GOARCH=amd64 go build -mod=vendor -o bin/darwin/${BINARY}-darwin-amd64 -ldflags "-X github.com/liamg/gitjacker/internal/app/version.Version=${TAG}" ./cmd/gitjacker/
mkdir -p bin/linux
GOOS=linux GOARCH=amd64 go build -mod=vendor -o bin/linux/${BINARY}-linux-amd64 -ldflags "-X github.com/liamg/gitjacker/internal/app/version.Version=${TAG}" ./cmd/gitjacker/
mkdir -p bin/windows
GOOS=windows GOARCH=amd64 go build -mod=vendor -o bin/windows/${BINARY}-windows-amd64.exe -ldflags "-X github.com/liamg/gitjacker/internal/app/version.Version=${TAG}" ./cmd/gitjacker/

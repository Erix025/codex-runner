BINDIR := dist
APPS := codexd codex-remote
VERSION ?= dev

LDFLAGS_CODEXD := -X codex-runner/internal/codexd/service.Version=$(VERSION)
LDFLAGS_CODEX_REMOTE := -X main.version=$(VERSION)

.PHONY: build build-linux build-darwin build-windows build-all clean

build:
	go build -ldflags "$(LDFLAGS_CODEXD)" ./cmd/codexd
	go build -ldflags "$(LDFLAGS_CODEX_REMOTE)" ./cmd/codex-remote

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEXD)" -o $(BINDIR)/linux-amd64/codexd ./cmd/codexd
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEX_REMOTE)" -o $(BINDIR)/linux-amd64/codex-remote ./cmd/codex-remote
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEXD)" -o $(BINDIR)/linux-arm64/codexd ./cmd/codexd
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEX_REMOTE)" -o $(BINDIR)/linux-arm64/codex-remote ./cmd/codex-remote

build-darwin:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEXD)" -o $(BINDIR)/darwin-amd64/codexd ./cmd/codexd
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEX_REMOTE)" -o $(BINDIR)/darwin-amd64/codex-remote ./cmd/codex-remote
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEXD)" -o $(BINDIR)/darwin-arm64/codexd ./cmd/codexd
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEX_REMOTE)" -o $(BINDIR)/darwin-arm64/codex-remote ./cmd/codex-remote

build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEXD)" -o $(BINDIR)/windows-amd64/codexd.exe ./cmd/codexd
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEX_REMOTE)" -o $(BINDIR)/windows-amd64/codex-remote.exe ./cmd/codex-remote
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEXD)" -o $(BINDIR)/windows-arm64/codexd.exe ./cmd/codexd
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_CODEX_REMOTE)" -o $(BINDIR)/windows-arm64/codex-remote.exe ./cmd/codex-remote

build-all: build-linux build-darwin build-windows

clean:
	rm -rf $(BINDIR)

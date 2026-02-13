BINDIR := dist
APPS := codexd codex-remote

.PHONY: build build-linux build-darwin build-windows build-all clean

build:
	go build ./cmd/codexd
	go build ./cmd/codex-remote

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINDIR)/linux-amd64/codexd ./cmd/codexd
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINDIR)/linux-amd64/codex-remote ./cmd/codex-remote
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINDIR)/linux-arm64/codexd ./cmd/codexd
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINDIR)/linux-arm64/codex-remote ./cmd/codex-remote

build-darwin:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINDIR)/darwin-amd64/codexd ./cmd/codexd
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINDIR)/darwin-amd64/codex-remote ./cmd/codex-remote
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINDIR)/darwin-arm64/codexd ./cmd/codexd
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINDIR)/darwin-arm64/codex-remote ./cmd/codex-remote

build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINDIR)/windows-amd64/codexd.exe ./cmd/codexd
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINDIR)/windows-amd64/codex-remote.exe ./cmd/codex-remote
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINDIR)/windows-arm64/codexd.exe ./cmd/codexd
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINDIR)/windows-arm64/codex-remote.exe ./cmd/codex-remote

build-all: build-linux build-darwin build-windows

clean:
	rm -rf $(BINDIR)

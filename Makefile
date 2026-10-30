PREFIX ?= $(HOME)/.local
BIN := devctl

.PHONY: build install test vet clean

build:
	go build -o $(BIN) ./cmd/devctl

# Installerar till ~/.local/bin och startar om servern om den körs
install:
	go build -o $(PREFIX)/bin/$(BIN) ./cmd/devctl
	-@$(PREFIX)/bin/$(BIN) stop >/dev/null 2>&1; true
	@echo "installerad: $(PREFIX)/bin/$(BIN)"

vet:
	gofmt -l . && go vet ./...

test:
	go test ./...

clean:
	rm -f $(BIN)

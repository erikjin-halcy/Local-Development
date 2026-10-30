PREFIX ?= $(HOME)/.local
BIN := devctl

.PHONY: build install app install-app test vet clean

build:
	go build -o $(BIN) ./cmd/devctl

# Installerar till ~/.local/bin och startar om servern om den körs
install:
	go build -o $(PREFIX)/bin/$(BIN) ./cmd/devctl
	-@$(PREFIX)/bin/$(BIN) stop >/dev/null 2>&1; true
	@echo "installerad: $(PREFIX)/bin/$(BIN)"

# macOS-appen: dist/Devctl.app med devctl inbakad
app:
	desktop/macos/build.sh

# Installerar appen i /Applications och CLI:t i ~/.local/bin
install-app: app install
	-@osascript -e 'tell application id "dev.devctl.app" to quit' >/dev/null 2>&1; sleep 1
	rm -rf /Applications/Devctl.app
	cp -R dist/Devctl.app /Applications/
	@echo "installerad: /Applications/Devctl.app"

vet:
	gofmt -l . && go vet ./...

test:
	go test ./...

clean:
	rm -rf $(BIN) dist

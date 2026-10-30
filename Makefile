PREFIX ?= $(HOME)/.local
BIN := devctl

.PHONY: build install app install-app test vet clean

build:
	go build -o $(BIN) ./cmd/devctl

# Installerar till ~/.local/bin och startar om servern om den körs
install:
	@mkdir -p $(PREFIX)/bin
	go build -o $(PREFIX)/bin/.$(BIN).new ./cmd/devctl
	@# Byt fil i stället för att skriva över en körande binär; stoppa sedan servern så startar appen om den
	mv -f $(PREFIX)/bin/.$(BIN).new $(PREFIX)/bin/$(BIN)
	-@$(PREFIX)/bin/$(BIN) stop >/dev/null 2>&1; true
	@echo "installerad: $(PREFIX)/bin/$(BIN)"

# macOS-appen: dist/Devctl.app med devctl inbakad
app:
	desktop/macos/build.sh

# Installerar motorn (~/.local/bin/devctl) och, bara om skalets kod ändrats, appen i /Applications.
# Appen startar själv om motorn när en ny version installerats. Att skalet lämnas orört gör att
# macOS inte frågar om behörigheter igen.
install-app: install
	@new=$$(cat desktop/macos/*.swift desktop/macos/Info.plist | shasum | cut -c1-12); \
	old=$$(cat /Applications/Devctl.app/Contents/Resources/shell.sha 2>/dev/null); \
	if [ "$$new" = "$$old" ]; then echo "appskalet är oförändrat; bara motorn uppdaterades"; else \
	  desktop/macos/build.sh && \
	  (osascript -e 'tell application id "dev.devctl.app" to quit' >/dev/null 2>&1; sleep 1; true) && \
	  rm -rf /Applications/Devctl.app && cp -R dist/Devctl.app /Applications/ && \
	  echo "installerad: /Applications/Devctl.app (nytt skal; macOS kan fråga om behörighet en gång)"; fi

vet:
	gofmt -l . && go vet ./...

test:
	go test ./...

clean:
	rm -rf $(BIN) dist

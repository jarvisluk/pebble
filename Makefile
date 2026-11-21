.PHONY: web build desktop app serve chat status test ios-tunnel android-tunnel

web:
	cd web && npm run build

build:
	go build -o bin/bot-hub ./cmd/bot-hub

# Desktop app (Wails, cgo): built natively on each OS.
desktop:
	go build -trimpath -ldflags="-s -w" -o bin/bot-hub-desktop ./cmd/bot-hub-desktop

APP = bin/Bot Hub.app
RES_BIN = $(APP)/Contents/Resources/bin
CODESIGN_IDENTITY ?=
app: desktop
	rm -rf "$(APP)" && mkdir -p "$(APP)/Contents/MacOS" "$(RES_BIN)"
	cp build/darwin/Info.plist "$(APP)/Contents/"
	cp -R build/darwin/*.lproj "$(APP)/Contents/Resources/"
	cp bin/bot-hub-desktop "$(APP)/Contents/MacOS/"
	@set -e; \
	for tool in agent-desktop agent-browser; do \
		src=$$(command -v "$$tool" 2>/dev/null || true); \
		if [ -z "$$src" ]; then \
			for cand in "/opt/homebrew/bin/$$tool" "/usr/local/bin/$$tool" "$$HOME/.nvm/versions/node"/*/bin/"$$tool"; do \
				if [ -x "$$cand" ]; then src="$$cand"; break; fi; \
			done; \
		fi; \
		if [ -z "$$src" ]; then \
			echo "ERROR: $$tool not found in PATH; install it before building $(APP)" >&2; \
			exit 1; \
		fi; \
		real=$$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$$src"); \
		cp "$$real" "$(RES_BIN)/$$tool"; \
		chmod +x "$(RES_BIN)/$$tool"; \
	done
	@arch=$$(uname -m); if [ "$$arch" = "x86_64" ]; then arch="x64"; fi; \
	bh=""; \
	for cand in "$$BOT_HOST_BIN" "$$(command -v bot-host 2>/dev/null || true)" \
		"$$HOME/.cache/opencode-bot/bin/bot-host-darwin-$$arch" \
		"$$HOME/codespace/opencode-bot/dist/bin/bot-host-darwin-$$arch"; do \
		if [ -n "$$cand" ] && [ -f "$$cand" ]; then \
			bh=$$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$$cand"); \
			break; \
		fi; \
	done; \
	if [ -n "$$bh" ]; then \
		cp "$$bh" "$(RES_BIN)/bot-host" && chmod +x "$(RES_BIN)/bot-host"; \
	else \
		echo "note: bot-host not found; $(APP) will use OS fallback for remote desktop"; \
	fi
	@IDENTITY="$(CODESIGN_IDENTITY)"; \
	if [ -z "$$IDENTITY" ]; then \
		IDENTITY=$$(security find-identity -v -p codesigning 2>/dev/null | awk '/Apple Development:/ {print $$2; exit}'); \
	fi; \
	if [ -z "$$IDENTITY" ]; then \
		IDENTITY=$$(security find-identity -v -p codesigning 2>/dev/null | awk '/Developer ID Application:/ {print $$2; exit}'); \
	fi; \
	if [ -z "$$IDENTITY" ]; then \
		IDENTITY="-"; \
	fi; \
	echo "Signing $(APP) with identity: $$IDENTITY"; \
	for f in "$(RES_BIN)"/*; do \
		[ -e "$$f" ] && codesign --force --sign "$$IDENTITY" "$$f"; \
	done; \
	codesign --force --sign "$$IDENTITY" "$(APP)"

serve:
	go run ./cmd/bot-hub serve

chat:
	go run ./cmd/bot-hub chat $(or $(BOT),assistant)

status:
	go run ./cmd/bot-hub status

test:
	go test ./...

ios-tunnel:
	./scripts/build-tunnel-ios.sh

android-tunnel:
	./scripts/build-tunnel-android.sh

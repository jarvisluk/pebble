.PHONY: web build desktop app app-fast app-wails serve chat status test check-i18n ios-tunnel android-tunnel android-native android-native-debug

GO_LDFLAGS ?= -s -w
APP_CONFIG ?= Release
UNIVERSAL ?= 0
DSYM ?= 0

web:
	cd web && npm run build

build:
	mkdir -p bin
	go build -ldflags="$(GO_LDFLAGS)" -o bin/pebble ./cmd/pebble
	ln -sf pebble bin/bot-hub

# Desktop app (Wails, cgo): used for Windows/Linux or legacy macOS packaging.
desktop:
	go build -trimpath -ldflags="-s -w" -o bin/bot-hub-desktop ./cmd/bot-hub-desktop

APP = bin/Pebble.app
RES_BIN = $(APP)/Contents/Resources/bin
CODESIGN_IDENTITY ?=

# Native SwiftUI macOS app (PebbleMac target + Go sidecar pebble).
app:
	@set -e; \
	arch=$$(uname -m); \
	dest_args=""; arch_args=""; dsym_args=""; \
	if [ "$(UNIVERSAL)" != "1" ]; then \
		dest_args="-destination platform=macOS,arch=$$arch"; \
		arch_args="ONLY_ACTIVE_ARCH=YES"; \
	fi; \
	if [ "$(DSYM)" != "1" ]; then \
		dsym_args="DEBUG_INFORMATION_FORMAT=dwarf"; \
	fi; \
	mkdir -p bin; \
	(go build -ldflags="$(GO_LDFLAGS)" -o bin/pebble ./cmd/pebble && ln -sf pebble bin/bot-hub) & \
	go_pid=$$!; \
	xcodebuild -project apps/apple/Pebble.xcodeproj -scheme PebbleMac -configuration "$(APP_CONFIG)" -derivedDataPath build/apple-mac \
		$$dest_args $$arch_args $$dsym_args \
		COMPILATION_CACHE_ENABLE_CACHING=YES \
		CODE_SIGN_IDENTITY=- CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=NO build & \
	xcode_pid=$$!; \
	wait "$$go_pid"; \
	wait "$$xcode_pid"
	rm -rf "$(APP)" && mkdir -p bin
	cp -R "build/apple-mac/Build/Products/$(APP_CONFIG)/Pebble.app" "$(APP)"
	mkdir -p "$(RES_BIN)"
	@if [ ! -f "$(APP)/Contents/Resources/AppIcon.icns" ] && [ -f build/darwin/AppIcon.icns ]; then \
		cp build/darwin/AppIcon.icns "$(APP)/Contents/Resources/AppIcon.icns"; \
	fi
	cp bin/pebble "$(RES_BIN)/pebble" && chmod +x "$(RES_BIN)/pebble"
	ln -sf pebble "$(RES_BIN)/bot-hub"
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
		IDENTITY=$$(security find-identity -v -p codesigning 2>/dev/null | awk '/Pebble Local Signing/ {print $$2; exit}'); \
	fi; \
	if [ -z "$$IDENTITY" ]; then \
		IDENTITY="-"; \
		echo "WARNING: No code signing identity found; falling back to ad-hoc signing (-). Every rebuild will reset macOS TCC permissions (Screen Recording / Documents / etc.). Install an Apple Development certificate or create a trusted 'Pebble Local Signing' certificate in Keychain." >&2; \
	fi; \
	echo "Signing $(APP) with identity: $$IDENTITY"; \
	for f in "$(RES_BIN)"/*; do \
		[ -f "$$f" ] && [ ! -L "$$f" ] && codesign --force --sign "$$IDENTITY" "$$f"; \
	done; \
	codesign --force --sign "$$IDENTITY" --entitlements apps/apple/Pebble/PebbleMac.entitlements "$(APP)"
	codesign --verify --deep --strict "$(APP)"

app-fast:
	$(MAKE) app APP_CONFIG=Debug

app-wails: desktop
	rm -rf "$(APP)" && mkdir -p "$(APP)/Contents/MacOS" "$(RES_BIN)"
	cp build/darwin/Info.plist "$(APP)/Contents/"
	cp build/darwin/AppIcon.icns "$(APP)/Contents/Resources/"
	cp -R build/darwin/*.lproj "$(APP)/Contents/Resources/"
	cp bin/bot-hub-desktop "$(APP)/Contents/MacOS/"
	ln -sf ../MacOS/bot-hub-desktop "$(RES_BIN)/pebble"
	ln -sf pebble "$(RES_BIN)/bot-hub"
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
		IDENTITY=$$(security find-identity -v -p codesigning 2>/dev/null | awk '/Pebble Local Signing/ {print $$2; exit}'); \
	fi; \
	if [ -z "$$IDENTITY" ]; then \
		IDENTITY="-"; \
		echo "WARNING: No code signing identity found; falling back to ad-hoc signing (-). Every rebuild will reset macOS TCC permissions (Screen Recording / Documents / etc.). Install an Apple Development certificate or create a trusted 'Pebble Local Signing' certificate in Keychain." >&2; \
	fi; \
	echo "Signing $(APP) with identity: $$IDENTITY"; \
	for f in "$(RES_BIN)"/*; do \
		[ -f "$$f" ] && [ ! -L "$$f" ] && codesign --force --sign "$$IDENTITY" "$$f"; \
	done; \
	codesign --force --sign "$$IDENTITY" "$(APP)"

serve:
	go run ./cmd/pebble serve

chat:
	go run ./cmd/pebble chat $(or $(BOT),assistant)

status:
	go run ./cmd/pebble status

test:
	go test ./...

check-i18n:
	go test -v ./internal/i18ncheck

ios-tunnel:
	./scripts/build-tunnel-ios.sh

android-tunnel:
	./scripts/build-tunnel-android.sh

android-native: android-tunnel
	cd apps/android && ./gradlew :app:assembleRelease

android-native-debug: android-tunnel
	cd apps/android && ./gradlew :app:assembleDebug

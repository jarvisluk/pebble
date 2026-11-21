.PHONY: build desktop app serve chat status test ios-tunnel android-tunnel

build:
	go build -o bin/bot-hub ./cmd/bot-hub

# Desktop app (Wails, cgo): built natively on each OS.
desktop:
	go build -trimpath -ldflags="-s -w" -o bin/bot-hub-desktop ./cmd/bot-hub-desktop

APP = bin/Bot Hub.app
app: desktop
	rm -rf "$(APP)" && mkdir -p "$(APP)/Contents/MacOS"
	cp build/darwin/Info.plist "$(APP)/Contents/"
	cp bin/bot-hub-desktop "$(APP)/Contents/MacOS/"
	codesign --force --sign - "$(APP)"

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

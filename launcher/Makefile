# MKYBOOT Launcher - reproducible build (run from launcher/)
#
#   make build   -> dist/MKYBOOT Launcher.exe (Windows x64 GUI, no console)
#   make test    -> unit tests (config, server client, security scans)
#   make vet     -> static checks
#   make uitest  -> drive the built exe through a real desktop session
#   make icon    -> regenerate assets/mkyboot.ico from generate.go
#
# Equivalent plain commands (no make required):
#
#   go test ./...
#   go vet ./...
#   go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe" .
#   powershell -ExecutionPolicy Bypass -File "./uitest/uitest.ps1"

GO      ?= go
DIST    := dist
BIN     := $(DIST)/MKYBOOT Launcher.exe
LDFLAGS := -s -w -H windowsgui

.PHONY: all build test vet uitest fmt icon res clean

all: vet test build

build: ## (rsrc_windows_amd64.syso is committed; run 'make res' to regenerate)
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN)" .

test:
	$(GO) test ./...

uitest: build ## scripted UI harness: needs a real desktop session and a
	# quiet screen (it inspects real pixels, hovers, clicks and checks the
	# tray). Prints PASS/FAIL per behaviour and writes snapshots to %TEMP%.
	# -File: the scripts call exit() to report failures via the exit code.
	powershell -NoProfile -ExecutionPolicy Bypass -File "./uitest/uitest.ps1"

vet:
	# -unsafeptr=false: the Win32 window procedures legitimately convert
	# lParam uintptr values (DRAWITEMSTRUCT pointers) to pointers; this is
	# the required pattern for Win32 callbacks in Go.
	$(GO) vet -unsafeptr=false ./...

fmt:
	gofmt -w .

icon:
	$(GO) run assets/generate.go assets/mkyboot.ico

res: icon ## embed icon/manifest/version resources (requires network for go-winres)
	$(GO) run github.com/tc-hib/go-winres@latest make --in winres/winres.json --arch amd64

clean:
	rm -rf $(DIST)

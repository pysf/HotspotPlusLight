.PHONY: reset-db release build-cross perf perf-quick perf-windows build build-windows build-all run set-password reset-password vet test ci clean generate

# Key used by `make run` when HSP_SECRET_KEY is not set. Created once, kept out of git.
DEV_KEY_FILE := .dev-secret-key

VERSION ?= dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/hotspotpluslight ./cmd/hotspotpluslight

# Windows 64-bit build (pure Go, no cgo): bin/hotspotpluslight.exe
build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/hotspotpluslight.exe ./cmd/hotspotpluslight

build-all: build build-windows

# The 9 platforms we ship (os/arch). Go has no windows/arm and no darwin/386.
# linux/arm is built with GOARM=7 (32-bit ARM with hardware float, e.g. Raspberry Pi 2 and newer).
# An ARMv6 build (Pi Zero, Pi 1) is not made.
PLATFORMS := linux/amd64 linux/386 linux/arm64 linux/arm windows/amd64 windows/386 windows/arm64 darwin/amd64 darwin/arm64

# Cross-build all platforms into dist/ (no packaging).
build-cross:
	@rm -rf dist && mkdir -p dist
	@set -e; for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; ext=""; [ $$os = windows ] && ext=".exe"; \
	  echo "build $$os/$$arch"; \
	  GOOS=$$os GOARCH=$$arch GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
	    -o dist/hotspotpluslight-$$os-$$arch$$ext ./cmd/hotspotpluslight; \
	done

# Release: build all platforms, pack each (.zip for windows, .tar.gz for the rest) and write dist/SHA256SUMS.
# File names have no version (hotspotpluslight-<os>-<arch>.tar.gz) so releases/latest/download/<name> works. The version is inside the binary.
# Each archive holds the binary (named hotspotpluslight[.exe]), README.md and LICENSE (if the file exists).
release: build-cross
	@set -e; cd dist; \
	for f in hotspotpluslight-*; do \
	  case $$f in *.zip|*.tar.gz) continue;; esac; \
	  base=$${f%.exe}; tmp=$$(mktemp -d); \
	  case $$f in \
	    *.exe) cp $$f $$tmp/hotspotpluslight.exe ;; \
	    *) cp $$f $$tmp/hotspotpluslight ;; \
	  esac; \
	  cp ../README.md $$tmp/; [ -f ../LICENSE ] && cp ../LICENSE $$tmp/ || true; \
	  case $$f in \
	    *.exe) (cd $$tmp && zip -q -r "$$OLDPWD/$$base.zip" .) ;; \
	    *) tar -czf $$base.tar.gz -C $$tmp . ;; \
	  esac; \
	  rm -rf $$tmp $$f; \
	done; \
	if command -v shasum >/dev/null 2>&1; then SUM="shasum -a 256"; else SUM=sha256sum; fi; \
	$$SUM *.zip *.tar.gz > SHA256SUMS; \
	ls -l

# Build and start the whole stack (admin :8081, RADIUS :1812/:1813) with data in ./data.
run: build
	@test -f $(DEV_KEY_FILE) || openssl rand -base64 32 > $(DEV_KEY_FILE)
	HSP_SECRET_KEY="$${HSP_SECRET_KEY:-$$(cat $(DEV_KEY_FILE))}" ./bin/hotspotpluslight serve

# Delete the dev database (hsp.db, -wal, -shm in $(DATA_DIR)) so the next `make run` starts fresh (admin/admin).
# Asks you to type "yes"; `make reset-db FORCE=1` skips the question. Stop `make run` first.
# The dev key file .dev-secret-key is kept on purpose: a fresh database works with the same key.
DATA_DIR ?= data
reset-db:
	@echo "Will delete: $(DATA_DIR)/hsp.db $(DATA_DIR)/hsp.db-wal $(DATA_DIR)/hsp.db-shm"
	@echo "Stop 'make run' first. The key file $(DEV_KEY_FILE) is kept."
	@set -e; \
	if [ -z "$(FORCE)" ]; then \
	  printf 'Type "yes" to continue: '; read ans </dev/tty; \
	  [ "$$ans" = yes ] || { echo "Cancelled. Nothing deleted."; exit 1; }; \
	fi; \
	rm -f "$(DATA_DIR)/hsp.db" "$(DATA_DIR)/hsp.db-wal" "$(DATA_DIR)/hsp.db-shm"; \
	echo "Database deleted. Next 'make run' starts fresh."

# Set (or reset) the admin password for the ./data database; prompts twice without echo.
# Rules (BR-20): at least 10 characters, not the username, "admin" or a common password. Logs out every open admin session.
set-password reset-password: build
	@test -f $(DEV_KEY_FILE) || openssl rand -base64 32 > $(DEV_KEY_FILE)
	HSP_SECRET_KEY="$${HSP_SECRET_KEY:-$$(cat $(DEV_KEY_FILE))}" ./bin/hotspotpluslight admin set-password

vet:
	go vet ./...

# Unit + end-to-end tests (the e2e tests start the real app themselves).
test:
	go test -race ./...

ci: vet test

clean:
	rm -rf bin dist

# Regenerate the templ components (the generated *_templ.go files are committed).
generate:
	go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate

# Capacity test (Phase 11d): raises the number of online users step by step. Takes about 25-30 minutes.
# Full run: writes docs/perf-results.md and the Performance section of README.md.
# Set HSP_PERF_MIN_USERS=n to fail when the max online users is below n (otherwise it never fails).
perf:
	go test -tags perf -run TestCapacity -v -timeout 60m ./perf

# Short check of the capacity test (two small steps of 30 s). Prints only; writes no file.
perf-quick:
	HSP_PERF_STEPS=500,1000 HSP_PERF_STEP_SECONDS=30 go test -tags perf -run TestCapacity -v -timeout 20m ./perf

# Windows files for the capacity test: bin/perf.test.exe and bin/hotspotpluslight.exe (keep them in one folder).
perf-windows: build-windows
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -tags perf -o bin/perf.test.exe ./perf

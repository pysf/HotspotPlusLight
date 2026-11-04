.PHONY: perf perf-quick perf-windows build build-windows build-all run set-password reset-password vet test ci clean generate

# Key used by `make run` when HSP_SECRET_KEY is not set. Created once, kept out of git.
DEV_KEY_FILE := .dev-secret-key

VERSION ?= dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/hotspotpluslight ./cmd/hotspotpluslight

# Windows 64-bit build (pure Go, no cgo): bin/hotspotpluslight.exe
build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/hotspotpluslight.exe ./cmd/hotspotpluslight

build-all: build build-windows

# Build and start the whole stack (public :8080, admin :8081, RADIUS :1812/:1813) with data in ./data.
run: build
	@test -f $(DEV_KEY_FILE) || openssl rand -base64 32 > $(DEV_KEY_FILE)
	HSP_SECRET_KEY="$${HSP_SECRET_KEY:-$$(cat $(DEV_KEY_FILE))}" ./bin/hotspotpluslight serve

# Set (or reset) the admin password for the ./data database; prompts twice without echo.
# Rules (BR-20): min 16 chars with upper, lower, digit and symbol. Logs out every open admin session.
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
	rm -rf bin

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

.PHONY: build run set-password reset-password vet test ci clean generate css css-check

# Key used by `make run` when HSP_SECRET_KEY is not set. Created once, kept out of git.
DEV_KEY_FILE := .dev-secret-key

build:
	go build -o bin/hotspotpluslight ./cmd/hotspotpluslight

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

ci: vet test css-check

clean:
	rm -rf bin

# Regenerate the templ components (the generated *_templ.go files are committed).
generate:
	go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate

# Tailwind CSS standalone CLI (no Node). Pinned; the binary goes to ./tools (gitignored) and is checked against the sha256 below.
TAILWIND_VERSION := v4.3.3
TAILWIND_OS := $(shell uname -s | tr A-Z a-z | sed 's/darwin/macos/')
TAILWIND_ARCH := $(shell uname -m | sed 's/x86_64/x64/; s/aarch64/arm64/')
TAILWIND_BIN := tools/tailwindcss
TAILWIND_SHA256_macos-arm64 := cdf646702987a743464dff4d9c60fd4480d1c1e73dd819a9a67f1078815dce9d
TAILWIND_SHA256_macos-x64 := 7922e0953f2110c05976e3bf58f14e643d90427575e766b7d433f5f80cbee7e1
TAILWIND_SHA256_linux-arm64 := 55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195
TAILWIND_SHA256_linux-x64 := dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a
CSS_IN := internal/web/ui/input.css
CSS_OUT := internal/web/static/app.css

TAILWIND_SHA256 := $(TAILWIND_SHA256_$(TAILWIND_OS)-$(TAILWIND_ARCH))

# Download (if missing) and always verify the pinned binary.
$(TAILWIND_BIN):
	@test -n "$(TAILWIND_SHA256)" || { echo "tailwind: no checksum for platform $(TAILWIND_OS)-$(TAILWIND_ARCH); add TAILWIND_SHA256_$(TAILWIND_OS)-$(TAILWIND_ARCH) to the Makefile"; exit 1; }
	@mkdir -p tools
	curl -sSfL -o $@ https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH)
	chmod +x $@

.PHONY: tailwind-verify
tailwind-verify: $(TAILWIND_BIN)
	@test -n "$(TAILWIND_SHA256)" || { echo "tailwind: no checksum for platform $(TAILWIND_OS)-$(TAILWIND_ARCH); add TAILWIND_SHA256_$(TAILWIND_OS)-$(TAILWIND_ARCH) to the Makefile"; exit 1; }
	@echo "$(TAILWIND_SHA256)  $(TAILWIND_BIN)" | shasum -a 256 -c - >/dev/null || { echo "tailwind: checksum mismatch for $(TAILWIND_BIN); deleting it"; rm -f $(TAILWIND_BIN); exit 1; }

# Build the committed stylesheet from the .templ and .go sources. Run after changing classes or input.css.
css: tailwind-verify
	$(TAILWIND_BIN) -i $(CSS_IN) -o $(CSS_OUT) --minify

# Fail if the committed stylesheet is not what `make css` would write.
css-check: tailwind-verify
	@$(TAILWIND_BIN) -i $(CSS_IN) -o tools/app.check.css --minify
	@cmp -s tools/app.check.css $(CSS_OUT) || { echo "$(CSS_OUT) is stale: run 'make css'"; exit 1; }

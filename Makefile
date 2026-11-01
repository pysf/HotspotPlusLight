.PHONY: build run vet test ci clean

# Key used by `make run` when HSP_SECRET_KEY is not set. Created once, kept out of git.
DEV_KEY_FILE := .dev-secret-key

build:
	go build -o bin/hotspotpluslight ./cmd/hotspotpluslight

# Build and start the whole stack (public :8080, admin :8081, RADIUS :1812/:1813) with data in ./data.
run: build
	@test -f $(DEV_KEY_FILE) || openssl rand -base64 32 > $(DEV_KEY_FILE)
	HSP_SECRET_KEY="$${HSP_SECRET_KEY:-$$(cat $(DEV_KEY_FILE))}" ./bin/hotspotpluslight serve

vet:
	go vet ./...

# Unit + end-to-end tests (the e2e tests start the real app themselves).
test:
	go test -race ./...

ci: vet test

clean:
	rm -rf bin

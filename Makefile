RUST = --manifest-path rust/Cargo.toml
COVERAGE_FLOOR = 90
JAVASCRIPT_EXTRA_PACKAGES = javascript/schema-reader-github javascript/schema-reader-s3
MOTO_IMAGE = ghcr.io/litebitus/motoserver/moto:5.2.6
MOTO_CREDENTIALS = AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_CONFIG_FILE=/dev/null AWS_SHARED_CREDENTIALS_FILE=/dev/null

.PHONY: build test test-go test-javascript test-rust

build: test
	cargo build $(RUST) --workspace --release
	npm --prefix javascript run build
	go -C go generate ./...
	go -C go build ./...

test:
	@container="$$(docker run -d --rm -p 127.0.0.1::5000 $(MOTO_IMAGE))" || exit 1; \
	trap 'docker rm -f "$$container" >/dev/null' EXIT; \
	endpoint="http://$$(docker port "$$container" 5000 | head -n 1)"; \
	for attempt in $$(seq 1 120); do \
		curl -sf "$$endpoint/moto-api/" >/dev/null && break; \
		sleep 0.5; \
	done; \
	curl -sf "$$endpoint/moto-api/" >/dev/null || { echo "Moto did not start" >&2; exit 1; }; \
	env -u AWS_PROFILE -u AWS_REGION -u AWS_DEFAULT_REGION AWS_ENDPOINT_URL="$$endpoint" $(MOTO_CREDENTIALS) \
		$(MAKE) test-rust test-javascript test-go

test-rust:
	cargo fmt $(RUST) --all --check
	cargo clippy $(RUST) --workspace --all-targets -- -D warnings
	cargo llvm-cov $(RUST) --workspace --fail-under-lines $(COVERAGE_FLOOR)

test-javascript:
	npm --prefix javascript ci
	npm --prefix javascript test
	@for package in $(JAVASCRIPT_EXTRA_PACKAGES); do \
		npm --prefix $$package ci && \
		npm --prefix $$package install --no-save --legacy-peer-deps=false "$(CURDIR)/javascript" && \
		npm --prefix $$package test || exit 1; \
	done

test-go:
	go -C go generate ./...
	@unformatted="$$(gofmt -l go)"; if [ -n "$$unformatted" ]; then echo "$$unformatted" >&2; exit 1; fi
	go -C go vet ./...
	go -C go test -count=1 -timeout 5s -coverpkg=./... -coverprofile=coverage.out ./...
	@go -C go tool cover -func=coverage.out | \
		awk -v floor=$(COVERAGE_FLOOR) '/^total:/ { sub(/%/, "", $$3); print "Go coverage: " $$3 "%"; exit !($$3 >= floor) }' || \
		{ echo "Go coverage is below $(COVERAGE_FLOOR)%" >&2; exit 1; }

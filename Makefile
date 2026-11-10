RUST = --manifest-path rust/Cargo.toml
COVERAGE_FLOOR = 90

.PHONY: build test test-go test-javascript test-rust

build: test
	cargo build $(RUST) --workspace --release
	npm --prefix javascript run build
	go -C go generate ./...
	go -C go build ./...

test: test-rust test-javascript test-go

test-rust:
	cargo fmt $(RUST) --all --check
	cargo clippy $(RUST) --workspace --all-targets -- -D warnings
	cargo llvm-cov $(RUST) --workspace --fail-under-lines $(COVERAGE_FLOOR)

test-javascript:
	npm --prefix javascript ci
	npm --prefix javascript test

test-go:
	go -C go generate ./...
	@unformatted="$$(gofmt -l go)"; if [ -n "$$unformatted" ]; then echo "$$unformatted" >&2; exit 1; fi
	go -C go vet ./...
	go -C go test -count=1 -timeout 5s -coverprofile=coverage.out ./...
	@go -C go tool cover -func=coverage.out | \
		awk -v floor=$(COVERAGE_FLOOR) '/^total:/ { sub(/%/, "", $$3); print "Go coverage: " $$3 "%"; exit !($$3 >= floor) }' || \
		{ echo "Go coverage is below $(COVERAGE_FLOOR)%" >&2; exit 1; }

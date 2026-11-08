RUST = --manifest-path rust/Cargo.toml
NPM_PACKAGE = $(shell node -p "require('./javascript/package.json').name")
COVERAGE_FLOOR = 90

.PHONY: build promote-npm test test-go test-javascript test-rust

build: test
	cargo build $(RUST) --release
	npm --prefix javascript run build
	go -C go generate ./...
	go -C go build ./...

test: test-rust test-javascript test-go

test-rust:
	cargo fmt $(RUST) --check
	cargo clippy $(RUST) --all-targets -- -D warnings
	cargo llvm-cov $(RUST) --fail-under-lines $(COVERAGE_FLOOR)

test-javascript:
	npm --prefix javascript ci
	npm --prefix javascript test

test-go:
	go -C go generate ./...
	@unformatted="$$(gofmt -l go)"; if [ -n "$$unformatted" ]; then echo "$$unformatted" >&2; exit 1; fi
	go -C go vet ./...
	go -C go test -timeout 5s -coverprofile=coverage.out ./...
	@go -C go tool cover -func=coverage.out | \
		awk -v floor=$(COVERAGE_FLOOR) '/^total:/ { sub(/%/, "", $$3); print "Go coverage: " $$3 "%"; exit !($$3 >= floor) }' || \
		{ echo "Go coverage is below $(COVERAGE_FLOOR)%" >&2; exit 1; }

promote-npm:
	@if [ -z "$(STAGE_ID)" ]; then \
		npm stage list $(NPM_PACKAGE); \
		echo "STAGE_ID is required: make promote-npm STAGE_ID=<stage id>" >&2; \
		exit 1; \
	fi
	npm stage view $(STAGE_ID)
	npm stage approve $(STAGE_ID)
	@echo "Promoted stage $(STAGE_ID) of $(NPM_PACKAGE) on npm."

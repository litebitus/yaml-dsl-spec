RUST = --manifest-path rust/Cargo.toml
NPM_PACKAGE = $(shell node -p "require('./javascript/package.json').name")

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
	cargo test $(RUST)

test-javascript:
	npm --prefix javascript ci
	npm --prefix javascript test

test-go:
	go -C go generate ./...
	@unformatted="$$(gofmt -l go)"; if [ -n "$$unformatted" ]; then echo "$$unformatted" >&2; exit 1; fi
	go -C go vet ./...
	go -C go test -timeout 5s ./...

promote-npm:
	@if [ -z "$(STAGE_ID)" ]; then \
		npm stage list $(NPM_PACKAGE); \
		echo "STAGE_ID is required: make promote-npm STAGE_ID=<stage id>" >&2; \
		exit 1; \
	fi
	npm stage view $(STAGE_ID)
	npm stage approve $(STAGE_ID)
	@echo "Promoted stage $(STAGE_ID) of $(NPM_PACKAGE) on npm."

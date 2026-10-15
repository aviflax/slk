.PHONY: build test fuzz lint run clean demo-gifs

BINARY=slk
BUILD_DIR=bin

build:
	go build -ldflags="-s -w" -trimpath -o $(BUILD_DIR)/$(BINARY) ./cmd/slk

test:
	go test ./... -v -race

# Runs every Fuzz target for FUZZTIME each (default 30s). Plain `make
# test` already replays the seed corpus and any saved crashers.
fuzz:
	scripts/fuzz.sh $(or $(FUZZTIME),30s)

lint:
	golangci-lint run ./...

run: build
	./$(BUILD_DIR)/$(BINARY)

clean:
	rm -rf $(BUILD_DIR)

# Renders docs/assets/demo/*.gif from their VHS tapes (settings.tape is
# shared, not rendered). Needs vhs: run inside `nix develop`, or see
# https://github.com/charmbracelet/vhs. Not run in CI.
demo-gifs: build
	@command -v vhs >/dev/null || { echo "vhs not found: run 'nix develop' or install charmbracelet/vhs"; exit 1; }
	@for tape in docs/assets/demo/*.tape; do \
		case $$tape in */settings.tape) continue;; esac; \
		echo "vhs $$tape"; vhs $$tape || exit 1; \
	done

BINARY_NAME := handler
BIN_DIR := bin
INSTALL_DIR := /usr/local/bin

.PHONY: build build-cli build-web install stop-running test clean dev

# Subcommands that run as long-lived servers. If any are running against the
# installed binary when we go to reinstall, they'd keep executing old code, so
# stop-running terminates them before the binary is replaced.
SERVER_CMDS := ui watcher tail

build-cli:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"

build-web:
	@if [ ! -f ui/package.json ]; then echo "Error: ui/package.json not found. Run from the repo root." && exit 1; fi
	@cd ui && npm install --silent && npm run build
	@echo "Built ui/dist/"

build: build-web build-cli

stop-running:
	@pattern="$(INSTALL_DIR)/$(BINARY_NAME) ($$(echo '$(SERVER_CMDS)' | tr ' ' '|'))"; \
	pids=$$(pgrep -f "$$pattern" 2>/dev/null || true); \
	if [ -n "$$pids" ]; then \
		echo "Stopping running $(BINARY_NAME) server(s): $$pids"; \
		kill -TERM $$pids 2>/dev/null || true; \
		sleep 2; \
		pids=$$(pgrep -f "$$pattern" 2>/dev/null || true); \
		if [ -n "$$pids" ]; then \
			echo "Force-killing remaining $(BINARY_NAME) server(s): $$pids"; \
			kill -KILL $$pids 2>/dev/null || true; \
		fi; \
	fi

install: build stop-running
	@cp $(BIN_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/.$(BINARY_NAME).tmp
	@chmod 755 $(INSTALL_DIR)/.$(BINARY_NAME).tmp
	@mv $(INSTALL_DIR)/.$(BINARY_NAME).tmp $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installed binary to $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo ""
ifdef NONINTERACTIVE
	@$(INSTALL_DIR)/$(BINARY_NAME) setup --yes
else
	@$(INSTALL_DIR)/$(BINARY_NAME) setup
endif

test:
	go test ./... -v

clean:
	rm -rf $(BIN_DIR) ui/dist ui/node_modules

dev:
	@command -v mprocs >/dev/null 2>&1 || { echo "Error: mprocs is required for dev mode. Install it: brew install mprocs"; exit 1; }
	@if command -v air >/dev/null 2>&1; then \
		mprocs --names "API: localhost:8420,Frontend: localhost:5173" "air -- ui --api-only" "cd ui && npm run dev"; \
	else \
		echo ""; \
		echo "  air not found — Go API server will not auto-reload on changes."; \
		if [ -x "$$(go env GOPATH)/bin/air" ]; then \
			echo "  air is installed at $$(go env GOPATH)/bin/air but not on PATH."; \
			echo "  Add to your shell rc: export PATH=\"\$$PATH:\$$(go env GOPATH)/bin\""; \
		else \
			echo "  Install it: go install github.com/air-verse/air@latest"; \
			echo "  Then add GOPATH/bin to PATH: export PATH=\"\$$PATH:\$$(go env GOPATH)/bin\""; \
		fi; \
		echo ""; \
		go build -o bin/handler .; \
		mprocs --names "API: localhost:8420,Frontend: localhost:5173" "bin/handler ui --api-only" "cd ui && npm run dev" || true; \
	fi
	@-lsof -ti :8420 2>/dev/null | xargs kill 2>/dev/null || true

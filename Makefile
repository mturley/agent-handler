BINARY_NAME := handler
BIN_DIR := bin
INSTALL_DIR := /usr/local/bin

.PHONY: build build-cli build-web install test clean dev

build-cli:
	@mkdir -p $(BIN_DIR)
	cabal build exe:$(BINARY_NAME)
	@cp "$$(cabal list-bin $(BINARY_NAME))" $(BIN_DIR)/$(BINARY_NAME)
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"
	@echo "Run 'make install' to install."

build-web:
	@if [ ! -f ui/package.json ]; then echo "Error: ui/package.json not found. Run from the repo root." && exit 1; fi
	@cd ui && npm install --silent && npm run build
	@echo "Built ui/dist/"

build: build-web build-cli

install:
	@test -f $(BIN_DIR)/$(BINARY_NAME) || (echo "Error: $(BIN_DIR)/$(BINARY_NAME) not found. Run 'make build' or 'make build-cli' first." && exit 1)
ifndef NONINTERACTIVE
	@if [ ! -d ui/dist ] || [ -z "$$(ls -A ui/dist 2>/dev/null)" ]; then \
		echo "Warning: Web UI not built — handler ui will not work."; \
		echo "Run 'make build' for a full build, or 'make build-cli' for CLI-only."; \
		printf "Continue? [y/N] "; \
		read answer; \
		case "$$answer" in [yY]*) ;; *) echo "Aborted."; exit 1;; esac; \
	fi
endif
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
	cabal test --test-show-details=direct

clean:
	rm -rf $(BIN_DIR) dist-newstyle ui/dist ui/node_modules

dev:
	@command -v mprocs >/dev/null 2>&1 || { echo "Error: mprocs is required for dev mode. Install it: brew install mprocs"; exit 1; }
	@cabal build exe:$(BINARY_NAME)
	@cp "$$(cabal list-bin $(BINARY_NAME))" bin/handler 2>/dev/null || { mkdir -p bin && cp "$$(cabal list-bin $(BINARY_NAME))" bin/handler; }
	@mprocs --names "API: localhost:8420,Frontend: localhost:5173" "bin/handler ui --api-only" "cd ui && npm run dev" || true
	@-lsof -ti :8420 2>/dev/null | xargs kill 2>/dev/null || true

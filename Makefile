BINARY_NAME := handler
BIN_DIR := bin
INSTALL_DIR := /usr/local/bin

.PHONY: build build-cli build-web install test clean dev

build-cli:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"
	@echo "Run 'make install' to install."

build-web:
	@if [ ! -f web/package.json ]; then echo "Error: web/package.json not found. Run from the repo root." && exit 1; fi
	@cd web && npm install --silent && npm run build
	@echo "Built web/dist/"

build: build-web build-cli

install:
	@test -f $(BIN_DIR)/$(BINARY_NAME) || (echo "Error: $(BIN_DIR)/$(BINARY_NAME) not found. Run 'make build' or 'make build-cli' first." && exit 1)
ifndef NONINTERACTIVE
	@if [ ! -d web/dist ] || [ -z "$$(ls -A web/dist 2>/dev/null)" ]; then \
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
	go test ./... -v

clean:
	rm -rf $(BIN_DIR) web/dist web/node_modules

dev:
	@echo "Starting dev servers..."
	@echo "  API server: http://localhost:8420"
	@echo "  Vite dev:   http://localhost:5173"
	@(cd web && npm run dev) & go run . ui --dev &
	@wait

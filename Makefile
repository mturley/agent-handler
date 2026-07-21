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
	go test ./... -v

clean:
	rm -rf $(BIN_DIR) ui/dist ui/node_modules

dev:
	@command -v mprocs >/dev/null 2>&1 || { echo "Error: mprocs is required for dev mode. Install it: brew install mprocs"; exit 1; }
	@if command -v air >/dev/null 2>&1; then \
		mprocs "air -- ui --dev" "cd ui && npm run dev"; \
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
		mprocs "go build -o bin/handler . && bin/handler ui --dev" "cd ui && npm run dev"; \
	fi

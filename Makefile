BINARY_NAME := handler
BIN_DIR := bin
INSTALL_DIR := /usr/local/bin

.PHONY: build install test clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"
	@echo "Run 'make install' to install."

install:
	@test -f $(BIN_DIR)/$(BINARY_NAME) || (echo "Error: $(BIN_DIR)/$(BINARY_NAME) not found. Run 'make build' first." && exit 1)
	@cp $(BIN_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@chmod 755 $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installed binary to $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo ""
	@$(INSTALL_DIR)/$(BINARY_NAME) setup

test:
	go test ./... -v

clean:
	rm -rf $(BIN_DIR)

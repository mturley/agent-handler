BINARY_NAME := handler
BIN_DIR := bin

.PHONY: build install clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"
	@echo "Run './$(BIN_DIR)/$(BINARY_NAME) install' to install, or 'make install' as a shortcut."

install: build
	./$(BIN_DIR)/$(BINARY_NAME) install

clean:
	rm -rf $(BIN_DIR)

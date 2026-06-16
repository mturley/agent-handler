BINARY_NAME := handler
BIN_DIR := bin
INSTALL_LINK := /usr/local/bin/$(BINARY_NAME)

.PHONY: build install clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"
	@echo ""
	@if [ -L "$(INSTALL_LINK)" ] || [ ! -e "$(INSTALL_LINK)" ]; then \
		ln -sf "$(CURDIR)/$(BIN_DIR)/$(BINARY_NAME)" "$(INSTALL_LINK)" && \
		echo "Symlinked $(INSTALL_LINK) -> $(CURDIR)/$(BIN_DIR)/$(BINARY_NAME)" ; \
	else \
		echo "$(INSTALL_LINK) already exists and is not a symlink." ; \
		echo "To link manually: ln -sf $(CURDIR)/$(BIN_DIR)/$(BINARY_NAME) $(INSTALL_LINK)" ; \
		echo "Or add $(CURDIR)/$(BIN_DIR) to your PATH." ; \
	fi

install: build
	$(BIN_DIR)/$(BINARY_NAME) install

clean:
	rm -rf $(BIN_DIR)
	@if [ -L "$(INSTALL_LINK)" ]; then \
		rm "$(INSTALL_LINK)" && echo "Removed symlink $(INSTALL_LINK)" ; \
	fi

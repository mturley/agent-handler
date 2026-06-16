BINARY_NAME := handler
BIN_DIR := bin
HANDLER_HOME := $(HOME)/.agent-handler
CLAUDE_SKILLS_DIR := $(HOME)/.claude/skills
CLAUDE_SETTINGS := $(HOME)/.claude/settings.json
USR_LOCAL_BIN := /usr/local/bin/$(BINARY_NAME)

SKILLS := inbox inbox_mode handler_register handler_emit handler_subscribe handler_snapshot handler_unregister
HOOKS := session_start.sh user_prompt_submit.sh pre_compact.sh

.PHONY: build install uninstall clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"
	@echo "Run 'make install' to install."

install:
	@test -f $(BIN_DIR)/$(BINARY_NAME) || (echo "Error: $(BIN_DIR)/$(BINARY_NAME) not found. Run 'make build' first." && exit 1)
	@echo ""
	@echo "agent-handler install will:"
	@echo ""
	@echo "  Create directory structure at $(HANDLER_HOME)"
	@echo "  Copy binary to $(HANDLER_HOME)/bin/$(BINARY_NAME)"
	@echo "  Symlink $(USR_LOCAL_BIN) -> $(HANDLER_HOME)/bin/$(BINARY_NAME)"
	@echo "  Copy hooks to $(HANDLER_HOME)/hooks/"
	@echo "  Copy skills to $(HANDLER_HOME)/skills/"
	@echo "  Symlink $(words $(SKILLS)) skills into $(CLAUDE_SKILLS_DIR)/"
	@echo "  Configure Claude Code hooks in $(CLAUDE_SETTINGS)"
	@echo ""
	@echo "  After install, the source repo can be deleted if desired."
	@echo ""
	@read -p "Proceed? [y/N] " answer && [ "$$answer" = "y" ] || (echo "Aborted." && exit 1)
	@echo ""
	@# Create directory structure
	@mkdir -p $(HANDLER_HOME)/bin $(HANDLER_HOME)/sessions $(HANDLER_HOME)/logs $(HANDLER_HOME)/hooks $(HANDLER_HOME)/skills
	@echo "  ✓ Created directory structure at $(HANDLER_HOME)"
	@# Initialize database
	@$(BIN_DIR)/$(BINARY_NAME) health >/dev/null 2>&1 || true
	@echo "  ✓ Initialized database"
	@echo ""
	@# Copy binary
	@cp $(BIN_DIR)/$(BINARY_NAME) $(HANDLER_HOME)/bin/$(BINARY_NAME)
	@chmod 755 $(HANDLER_HOME)/bin/$(BINARY_NAME)
	@echo "  ✓ Copied binary to $(HANDLER_HOME)/bin/$(BINARY_NAME)"
	@# Symlink to /usr/local/bin
	@rm -f $(USR_LOCAL_BIN)
	@ln -s $(HANDLER_HOME)/bin/$(BINARY_NAME) $(USR_LOCAL_BIN) 2>/dev/null \
		&& echo "  ✓ Symlinked $(USR_LOCAL_BIN) -> $(HANDLER_HOME)/bin/$(BINARY_NAME)" \
		|| echo "  ⚠ Could not symlink $(USR_LOCAL_BIN) — add $(HANDLER_HOME)/bin to your PATH"
	@echo ""
	@# Copy hooks
	@for hook in $(HOOKS); do \
		cp hooks/$$hook $(HANDLER_HOME)/hooks/$$hook && \
		chmod 755 $(HANDLER_HOME)/hooks/$$hook && \
		echo "  ✓ Copied hook $$hook"; \
	done
	@echo ""
	@# Clean up stale skills from previous installs
	@for dir in $(HANDLER_HOME)/skills/*/; do \
		name=$$(basename "$$dir"); \
		found=0; \
		for skill in $(SKILLS); do \
			[ "$$name" = "$$skill" ] && found=1 && break; \
		done; \
		if [ $$found -eq 0 ]; then \
			rm -rf "$$dir"; \
			rm -f "$(CLAUDE_SKILLS_DIR)/$$name"; \
			echo "  ✓ Removed stale skill $$name"; \
		fi; \
	done 2>/dev/null || true
	@# Copy skills and create symlinks
	@mkdir -p $(CLAUDE_SKILLS_DIR)
	@for skill in $(SKILLS); do \
		rm -rf $(HANDLER_HOME)/skills/$$skill && \
		cp -r skills/$$skill $(HANDLER_HOME)/skills/$$skill && \
		rm -f $(CLAUDE_SKILLS_DIR)/$$skill && \
		ln -s $(HANDLER_HOME)/skills/$$skill $(CLAUDE_SKILLS_DIR)/$$skill && \
		echo "  ✓ $$skill -> $(HANDLER_HOME)/skills/$$skill"; \
	done
	@echo ""
	@# Configure Claude Code hooks in settings.json
	@python3 -c '\
import json, os, sys; \
path = os.path.expanduser("$(CLAUDE_SETTINGS)"); \
s = json.load(open(path)) if os.path.exists(path) else {}; \
hooks_dir = os.path.expanduser("$(HANDLER_HOME)/hooks"); \
h = s.get("hooks", {}); \
entries = {"SessionStart": ("session_start.sh", 10), "UserPromptSubmit": ("user_prompt_submit.sh", 5), "PreCompact": ("pre_compact.sh", 10)}; \
[h.__setitem__(event, [{"matcher": "", "hooks": [{"type": "command", "command": os.path.join(hooks_dir, script), "timeout": timeout}]}]) for event, (script, timeout) in entries.items()]; \
s["hooks"] = h; \
open(path, "w").write(json.dumps(s, indent=2) + "\n"); \
[print(f"  ✓ {event} -> {os.path.join(hooks_dir, script)}") for event, (script, _) in entries.items()]'
	@echo ""
	@echo "✓ Installation complete!"
	@echo ""
	@echo "  All files installed to $(HANDLER_HOME)"
	@echo "  The source repo can be deleted if desired without affecting the installation."
	@echo "  To update, pull changes and run 'make build && make install' again."
	@echo ""
	@echo "Test with: handler status"

clean:
	rm -rf $(BIN_DIR)

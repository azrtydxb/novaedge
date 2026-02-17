# NovaEdge WASM Plugin Build System
# Requires TinyGo: https://tinygo.org/getting-started/install/
#
# Usage:
#   make              # Build all plugins
#   make geoip        # Build single plugin
#   make clean        # Remove all build artifacts
#   make check        # Verify TinyGo installation
#   make list         # List all available plugins

TINYGO          ?= tinygo
TINYGO_VERSION  := 0.34
WASM_TARGET     := wasi
SCHEDULER       := none
GC              := conservative
OPT_LEVEL       := z

OUTPUT_DIR      := ../../bin/plugins
PLUGIN_DIRS     := geoip requestid bodyrewrite customauth schemavalidation \
                   piimask graphqlprotect abtesting businessmetrics \
                   canaryanalysis multitenant cacheinvalidation protocoltransform
PLUGIN_WASMS    := $(addprefix $(OUTPUT_DIR)/,$(addsuffix .wasm,$(PLUGIN_DIRS)))

# TinyGo build flags for small, optimised WASM modules.
TINYGO_FLAGS := \
	-target=$(WASM_TARGET) \
	-scheduler=$(SCHEDULER) \
	-gc=$(GC) \
	-opt=$(OPT_LEVEL) \
	-no-debug

.PHONY: all clean check list $(PLUGIN_DIRS)

all: $(PLUGIN_WASMS)
	@echo "All plugins built successfully."

# Individual plugin targets.
$(PLUGIN_DIRS): %: $(OUTPUT_DIR)/%.wasm

$(OUTPUT_DIR)/%.wasm: %/main.go | $(OUTPUT_DIR)
	@echo "Building $* plugin -> $@"
	$(TINYGO) build $(TINYGO_FLAGS) -o $@ ./$*

$(OUTPUT_DIR):
	@mkdir -p $(OUTPUT_DIR)

clean:
	@echo "Removing build artifacts..."
	@rm -f $(PLUGIN_WASMS)
	@rmdir $(OUTPUT_DIR) 2>/dev/null || true
	@echo "Clean complete."

check:
	@echo "Checking TinyGo installation..."
	@$(TINYGO) version
	@echo ""
	@echo "Checking WASM target support..."
	@$(TINYGO) info -target=$(WASM_TARGET) > /dev/null 2>&1 && \
		echo "WASI target: supported" || \
		echo "WASI target: NOT supported (update TinyGo)"

list:
	@echo "Available plugins:"
	@for p in $(PLUGIN_DIRS); do \
		echo "  - $$p"; \
	done

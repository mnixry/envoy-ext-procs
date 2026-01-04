# Build configuration
GO ?= go
GOFLAGS ?= -v
LDFLAGS ?= -s -w
BUILD_DIR ?= bin

# Discover commands from cmd/
CMDS := $(notdir $(wildcard cmd/*))

.PHONY: all build clean $(CMDS)

all: build

build: $(CMDS)

$(CMDS):
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$@ ./cmd/$@

clean:
	rm -rf $(BUILD_DIR)

# Development helpers
.PHONY: tidy fmt vet

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

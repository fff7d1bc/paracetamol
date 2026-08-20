APP ?= rocmplete
DISPLAY_NAME ?= ROCmplete
GO_PACKAGE := .
BUILD_ROOT := $(CURDIR)/build
GOTOOLCHAIN ?= local
GOOS ?= $(shell GOENV=off GOTOOLCHAIN=$(GOTOOLCHAIN) go env GOOS)
GOARCH ?= $(shell GOENV=off GOTOOLCHAIN=$(GOTOOLCHAIN) go env GOARCH)
PLATFORM := $(GOOS)-$(GOARCH)
PLATFORM_BUILD_DIR := $(BUILD_ROOT)/$(PLATFORM)
BIN_DIR := $(PLATFORM_BUILD_DIR)/bin
BIN := $(BIN_DIR)/$(APP)
STATIC_BIN := $(BIN_DIR)/$(APP)-static
GO_SOURCE_ROOTS := main.go internal $(wildcard cmd)
GO_SOURCES := $(shell find $(GO_SOURCE_ROOTS) -type f -name '*.go' -print)
GO_PACKAGES := . ./internal/... $(if $(wildcard cmd),./cmd/...)
GO_MODULE_FILES := go.mod $(wildcard go.sum)
GOCACHE := $(PLATFORM_BUILD_DIR)/gocache
GOMODCACHE := $(PLATFORM_BUILD_DIR)/gomodcache
GOPATH := $(PLATFORM_BUILD_DIR)/gopath
GOTMPDIR := $(PLATFORM_BUILD_DIR)/tmp
GOTELEMETRYDIR := $(PLATFORM_BUILD_DIR)/telemetry
GOENV := off
GOFLAGS := -modcacherw -buildvcs=false
IDENTITY_LDFLAGS := -X 'rocmplete/internal/identity.CommandName=$(APP)' -X 'rocmplete/internal/identity.DisplayName=$(DISPLAY_NAME)'

export GOOS
export GOARCH
export GOCACHE
export GOMODCACHE
export GOPATH
export GOTMPDIR
export GOTELEMETRYDIR
export GOENV
export GOFLAGS
export GOTOOLCHAIN
export GOTELEMETRY=off

.PHONY: all build static test check clean print-bin launcher-binary

all: build

build: $(BIN)

static: $(STATIC_BIN)

test: | $(GOTMPDIR)
	go test $(GO_PACKAGES)

check: | $(GOTMPDIR)
	@test -z "$$(gofmt -l $(GO_SOURCES))" || { \
		printf '%s\n' 'Go source needs formatting:'; \
		gofmt -l $(GO_SOURCES); \
		exit 1; \
	}
	go vet $(GO_PACKAGES)

print-bin:
	@printf '%s\n' "$(BIN)"

launcher-binary: build
	@printf '%s\n' "$(BIN)"

$(BIN): $(GO_MODULE_FILES) $(GO_SOURCES) | $(BIN_DIR) $(GOTMPDIR)
	@temporary="$$(mktemp "$(BIN_DIR)/.$(APP).XXXXXX")"; \
	trap 'rm -f "$$temporary"' EXIT INT TERM HUP; \
	go build -trimpath -ldflags "$(IDENTITY_LDFLAGS)" -o "$$temporary" "$(GO_PACKAGE)"; \
	chmod 0755 "$$temporary"; \
	mv -f "$$temporary" "$(BIN)"; \
	trap - EXIT INT TERM HUP

$(STATIC_BIN): $(GO_MODULE_FILES) $(GO_SOURCES) | $(BIN_DIR) $(GOTMPDIR)
	@temporary="$$(mktemp "$(BIN_DIR)/.$(APP)-static.XXXXXX")"; \
	trap 'rm -f "$$temporary"' EXIT INT TERM HUP; \
	CGO_ENABLED=0 go build -trimpath -tags 'netgo osusergo' \
		-ldflags "-s -w -buildid= $(IDENTITY_LDFLAGS)" \
		-o "$$temporary" "$(GO_PACKAGE)"; \
	chmod 0755 "$$temporary"; \
	mv -f "$$temporary" "$(STATIC_BIN)"; \
	trap - EXIT INT TERM HUP
	@printf '%s\n' "static binary: $(STATIC_BIN)"

$(BIN_DIR) $(GOTMPDIR):
	mkdir -p "$(BIN_DIR)" "$(GOCACHE)" "$(GOMODCACHE)" \
		"$(GOPATH)" "$(GOTMPDIR)" "$(GOTELEMETRYDIR)"

clean:
	@test "$(BUILD_ROOT)" = "$(CURDIR)/build"
	chmod -R u+w "$(BUILD_ROOT)" 2>/dev/null || true
	rm -rf -- "$(BUILD_ROOT)"

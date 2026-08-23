PRODUCT_ID ?= paracetamol
DISPLAY_NAME ?= Paracetamol
STATE_NAMESPACE ?= $(PRODUCT_ID)
IMAGE_NAMESPACE ?= localhost/$(PRODUCT_ID)
LABEL_NAMESPACE ?= io.github.fff7d1bc.$(PRODUCT_ID)
# Empty means the Go identity package derives the normalized prefix from the
# command name. A build may still select an explicit spelling when needed.
ENV_PREFIX ?=
GO_PACKAGE := .
BUILD_ROOT := $(CURDIR)/build
GOTOOLCHAIN ?= local
# Platform discovery must not select or download the project toolchain before
# the repository-local module and telemetry state below is active.
HOST_GO_ENV := GOENV=off GO111MODULE=off GOTOOLCHAIN=local GOTELEMETRY=off
HOST_GOOS := $(shell $(HOST_GO_ENV) go env GOOS)
HOST_GOARCH := $(shell $(HOST_GO_ENV) go env GOARCH)
GOOS ?= $(HOST_GOOS)
GOARCH ?= $(HOST_GOARCH)
PLATFORM := $(GOOS)-$(GOARCH)
PLATFORM_BUILD_DIR := $(BUILD_ROOT)/$(PLATFORM)
BIN_DIR := $(PLATFORM_BUILD_DIR)/bin
BIN := $(BIN_DIR)/$(PRODUCT_ID)
STATIC_BIN := $(BIN_DIR)/$(PRODUCT_ID)-static
# Keep host packages explicit: frozen evaluation modules and ignored local
# content are repository inputs, not control-plane packages.
GO_SOURCE_ROOTS := main.go internal tools $(wildcard cmd)
GO_SOURCES := $(shell find $(GO_SOURCE_ROOTS) -type f -name '*.go' -print)
GO_PACKAGES := . ./internal/... ./tools/... $(if $(wildcard cmd),./cmd/...)
GO_MODULE_FILES := go.mod $(wildcard go.sum)
GO_BUILD_INPUTS := Makefile $(GO_MODULE_FILES) $(GO_SOURCES)
GOCACHE := $(PLATFORM_BUILD_DIR)/gocache
GOMODCACHE := $(PLATFORM_BUILD_DIR)/gomodcache
GOPATH := $(PLATFORM_BUILD_DIR)/gopath
GOTMPDIR := $(PLATFORM_BUILD_DIR)/tmp
GOTELEMETRYDIR := $(PLATFORM_BUILD_DIR)/telemetry
GOENV := off
GOFLAGS := -modcacherw -buildvcs=false
IDENTITY_LDFLAGS := -X 'paracetamol/internal/identity.CommandName=$(PRODUCT_ID)' \
	-X 'paracetamol/internal/identity.DisplayName=$(DISPLAY_NAME)' \
	-X 'paracetamol/internal/identity.StateNamespace=$(STATE_NAMESPACE)' \
	-X 'paracetamol/internal/identity.EnvPrefix=$(ENV_PREFIX)' \
	-X 'paracetamol/internal/identity.ImageNamespace=$(IMAGE_NAMESPACE)' \
	-X 'paracetamol/internal/identity.LabelNamespace=$(LABEL_NAMESPACE)'

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

.PHONY: all build static test race check clean print-bin launcher-binary

all: build

build: $(BIN)

static: $(STATIC_BIN)

test: | $(GOTMPDIR)
	go test $(GO_PACKAGES)
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests

race:
	@test "$(HOST_GOOS)" = linux && test "$(PLATFORM)" = "$(HOST_GOOS)-$(HOST_GOARCH)" || { \
		printf '%s\n' 'make race supports only the native Linux target'; \
		exit 2; \
	}
	mkdir -p "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" \
		"$(GOTMPDIR)" "$(GOTELEMETRYDIR)"
	CGO_ENABLED=1 go test -race $(GO_PACKAGES)

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

$(BIN): $(GO_BUILD_INPUTS) | $(BIN_DIR) $(GOTMPDIR)
	@test "$$(go env CGO_ENABLED)" = 1 || { \
		printf '%s\n' 'normal builds require CGO; use make static for the pure-Go binary'; \
		exit 2; \
	}
	@temporary="$$(mktemp "$(BIN_DIR)/.$(PRODUCT_ID).XXXXXX")"; \
	trap 'rm -f "$$temporary"' EXIT INT TERM HUP; \
	go build -trimpath -ldflags "$(IDENTITY_LDFLAGS)" -o "$$temporary" "$(GO_PACKAGE)"; \
	chmod 0755 "$$temporary"; \
	mv -f "$$temporary" "$(BIN)"; \
	trap - EXIT INT TERM HUP

$(STATIC_BIN): $(GO_BUILD_INPUTS) | $(BIN_DIR) $(GOTMPDIR)
	@temporary="$$(mktemp "$(BIN_DIR)/.$(PRODUCT_ID)-static.XXXXXX")"; \
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

# Every target in this Makefile is a command, not a file recipe: none of them
# produces a file of its own name. All of them must therefore be declared
# .PHONY, and internal/installcheck.TestMakefilePhonyCoversEveryTarget
# enforces exactly that, over the full inventory rather than a sample.
#
# `test` is why this is a defect rather than a style nit. The repository has a
# `test/` directory, so while the target was undeclared make considered it
# already satisfied: `make test` printed "'test' is up to date" and ran
# nothing, silently, for as long as AGENTS.md documented it as `go test ./...`.
# The others worked only because no directory happens to share their names.
.PHONY: all deps tidy
.PHONY: schemas
.PHONY: build
.PHONY: build-linux-amd64 build-linux-arm64
.PHONY: run dev clean
.PHONY: test fmt lint
.PHONY: build-e2e e2e
.PHONY: install uninstall
.PHONY: bump

# Binary names
BINARY_NAME=chairlift
HELPER_NAME=chairlift-updex-helper
UBLUE_HELPER_NAME=chairlift-ublue-helper

# Build directory
BUILD_DIR=build

# Installation directories
#
# PREFIX defaults to /usr, matching both the fixed absolute path pkexec
# matches against PolicyKit's org.freedesktop.policykit.exec.path annotation
# (data/io.projectbluefin.chairlift.updex.policy, internal/updex.HelperPath) and
# the layout .goreleaser.yaml's nFPM packages already install to. PolicyKit
# itself only ever reads actions from /usr/share/polkit-1/actions — it does
# not consult PREFIX or XDG_DATA_DIRS — so installing under any other PREFIX
# means the .policy files land somewhere polkit never looks. Override PREFIX
# for a non-privileged dev install (e.g.
# `make install PREFIX=$$HOME/.local`), but understand that the
# PolicyKit-authenticated updex helper path then no longer resolves to the
# fixed exec.path annotation. DESTDIR (below) still layers under PREFIX
# unchanged, for staged/packaged installs.
PREFIX ?= /usr
BINDIR = $(PREFIX)/bin
DATADIR = $(PREFIX)/share
ICONSDIR = $(DATADIR)/icons
APPLICATIONSDIR = $(DATADIR)/applications
CONFIGDIR = $(DATADIR)/chairlift
SCHEMASDIR = $(DATADIR)/glib-2.0/schemas
POLKITACTIONSDIR = $(DATADIR)/polkit-1/actions
POLKITRULESDIR = $(DATADIR)/polkit-1/rules.d

# Go parameters - use Homebrew's Go if available, otherwise fall back to system Go
HOMEBREW_GO=/home/linuxbrew/.linuxbrew/bin/go
GOCMD=$(shell if [ -x $(HOMEBREW_GO) ]; then echo $(HOMEBREW_GO); else echo go; fi)
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

# CGO is not needed with puregotk!
CGO_ENABLED=0

all: deps build

deps:
	$(GOMOD) download

tidy:
	$(GOMOD) tidy

# `schemas` is a prerequisite because a binary that writes a key the compiled
# schema does not carry fails at runtime with "No such key" — a confusing way
# to discover build/schemas went stale after the gschema gained one.
build: schemas
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(E2E_COVER_FLAG) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/chairlift
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(E2E_COVER_FLAG) -o $(BUILD_DIR)/$(HELPER_NAME) ./cmd/chairlift-updex-helper
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(E2E_COVER_FLAG) -o $(BUILD_DIR)/$(UBLUE_HELPER_NAME) ./cmd/chairlift-ublue-helper

run: build
	./$(BUILD_DIR)/$(BINARY_NAME) --dry-run

clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)

test:
	$(GOTEST) -v ./...

# The screenshot walkthrough needs ChairLift to render the Bluefin-family
# rows on a host that is not a Bluefin system, which requires redirecting the
# image-descriptor read. That override is compiled in only under this build
# tag, so no released binary can honor it (internal/app/imageinfo_override.go
# explains why). `make ci` builds untagged, which is what proves it.
E2E_TAGS=chairlift_e2e

# The tagged GUI is written to its own subdirectory rather than over
# $(BUILD_DIR)/chairlift. The staged-install E2E test runs `make install`,
# which depends on `build` and would otherwise replace the tagged binary with
# an untagged one mid-suite, silently disabling the walkthrough's Bluefin
# rows.
E2E_BUILD_DIR=$(BUILD_DIR)/e2e

# E2E_COVERDIR, when set, makes build and build-e2e produce coverage-instrumented
# binaries and makes the e2e target collect what they recorded. It is empty
# by default so a developer running `make e2e` gets the same plain binaries
# as before.
E2E_COVERDIR?=
E2E_COVER_FLAG=$(if $(E2E_COVERDIR),-cover -covermode=atomic,)

build-e2e: build
	@mkdir -p $(E2E_BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(E2E_COVER_FLAG) -tags $(E2E_TAGS) -o $(E2E_BUILD_DIR)/$(BINARY_NAME) ./cmd/chairlift

# Regenerate the documentation walkthrough screenshots in docs/screenshots/
# from the real application, then remove the capture byproducts so only the
# PNGs are left to commit.
#
# It cannot join `make ci`: it needs a display, GTK, and the X utilities. It
# is also deliberately not run per-push — font hinting and GTK point releases
# move pixels, so regenerating on every commit would churn the repository for
# no signal. Run it when a feature's appearance actually changes.
#
# The referential check that every page has a screenshot and every screenshot
# is referenced by docs/walkthrough.md is a pure-Go test in
# internal/installcheck, so that half does run in `make ci`.
.PHONY: screenshots
screenshots: build-e2e schemas
	@mkdir -p $(SCREENSHOT_DIR)
	CHAIRLIFT_E2E_BUILD_DIR=$(abspath $(BUILD_DIR)) \
		CHAIRLIFT_WALKTHROUGH_DIR=$(abspath $(SCREENSHOT_DIR)) \
		CHAIRLIFT_SCHEMA_DIR=$(abspath $(BUILD_DIR))/schemas \
		$(GOTEST) -count=1 -run TestWalkthroughScreenshots ./test/e2e
	@test -n "$(SCREENSHOT_DIR)" || { echo "SCREENSHOT_DIR is empty; refusing to clean" >&2; exit 1; }
	@rm -rf "$(SCREENSHOT_DIR)/home" $(SCREENSHOT_DIR)/*.xwd \
		"$(SCREENSHOT_DIR)/chairlift.log" "$(SCREENSHOT_DIR)/window-geometry.env" \
		"$(SCREENSHOT_DIR)/image-info.json"
	@echo "==> screenshots written to $(SCREENSHOT_DIR)"

SCREENSHOT_DIR=docs/screenshots

# End-to-end smoke tests require GTK4, Libadwaita, dbus-run-session, and Xvfb;
# the screenshot walkthrough additionally requires Xvfb, xdotool, xdpyinfo,
# and xwd. They run separately from ci because the ordinary unit-test gate is
# intentionally usable on hosts without those runtime libraries.
#
# Only the GUI is built with E2E_TAGS. Both privileged helpers are built
# exactly as they ship, so the boundary assertions in test/e2e exercise the
# real binaries.
# The compiled schemas come along for the same reason `screenshots` builds
# them: without CHAIRLIFT_SCHEMA_DIR the Livery page logs "the Livery
# settings schema is not installed" and renders without its saved state, and
# the walkthrough captures this target uploads are exactly what ships in
# docs/screenshots.
e2e: build-e2e schemas
ifeq ($(E2E_COVERDIR),)
	CHAIRLIFT_E2E_BUILD_DIR=$(abspath $(BUILD_DIR)) \
		CHAIRLIFT_SCHEMA_DIR=$(abspath $(BUILD_DIR))/schemas \
		$(GOTEST) -v ./test/e2e
else
	@rm -rf "$(E2E_COVERDIR)" && mkdir -p "$(E2E_COVERDIR)"
	CHAIRLIFT_E2E_BUILD_DIR=$(abspath $(BUILD_DIR)) \
		CHAIRLIFT_SCHEMA_DIR=$(abspath $(BUILD_DIR))/schemas \
		GOCOVERDIR=$(abspath $(E2E_COVERDIR)) \
		$(GOTEST) -v ./test/e2e
	$(GOCMD) tool covdata textfmt -i=$(abspath $(E2E_COVERDIR)) -o=e2e-coverage.out
	@echo "==> e2e statement coverage written to e2e-coverage.out"
endif

# Development build with race detector (requires CGO)
dev:
	CGO_ENABLED=1 $(GOBUILD) -race -o $(BUILD_DIR)/$(BINARY_NAME)-dev ./cmd/chairlift


# Format code
fmt:
	gofmt -s -w .

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Cross-compile for different architectures
build-linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/chairlift

build-linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/chairlift

# Install the application
install: build
	# Install binary
	install -Dm755 $(BUILD_DIR)/$(BINARY_NAME) $(DESTDIR)$(BINDIR)/$(BINARY_NAME)
	# Install wrapper script
	install -Dm755 data/chairlift-wrapper.sh $(DESTDIR)$(BINDIR)/chairlift-wrapper
	# Install the Livery and Updates GSettings schemas, then recompile the system schema
	# cache so `gsettings` can see them.
	install -Dm644 data/io.projectbluefin.chairlift.livery.gschema.xml $(DESTDIR)$(SCHEMASDIR)/io.projectbluefin.chairlift.livery.gschema.xml
	install -Dm644 data/io.projectbluefin.chairlift.updates.gschema.xml $(DESTDIR)$(SCHEMASDIR)/io.projectbluefin.chairlift.updates.gschema.xml
	# Only for a direct install. Under DESTDIR the tree is a staging area
	# holding this schema alone, so compiling there would produce a
	# shipping that file would overwrite the system cache and break GSettings
	# for every other application. Packages run glib-compile-schemas from
	# their postinstall scriptlet instead; see packaging/postinstall.sh.
	@if [ -z "$(DESTDIR)" ]; then glib-compile-schemas $(SCHEMASDIR); fi
	# Install desktop file
	install -Dm644 data/io.projectbluefin.chairlift.desktop $(DESTDIR)$(APPLICATIONSDIR)/io.projectbluefin.chairlift.desktop
	# Install package-maintainer defaults; /etc/chairlift/config.yml remains the administrator-owned override
	install -Dm644 config.yml $(DESTDIR)$(CONFIGDIR)/config.yml
	# Install icons
	install -Dm644 data/icons/hicolor/scalable/apps/io.projectbluefin.chairlift.svg $(DESTDIR)$(ICONSDIR)/hicolor/scalable/apps/io.projectbluefin.chairlift.svg
	install -Dm644 data/icons/hicolor/symbolic/apps/io.projectbluefin.chairlift-symbolic.svg $(DESTDIR)$(ICONSDIR)/hicolor/symbolic/apps/io.projectbluefin.chairlift-symbolic.svg
	# Install updex helper binary
	install -Dm755 $(BUILD_DIR)/$(HELPER_NAME) $(DESTDIR)$(BINDIR)/$(HELPER_NAME)
	# Install Bluefin-family (channel switch / developer mode) helper binary
	install -Dm755 $(BUILD_DIR)/$(UBLUE_HELPER_NAME) $(DESTDIR)$(BINDIR)/$(UBLUE_HELPER_NAME)
	# Install the documented release-channel table template. The live file is
	# $(CONFIGDIR)/channels.yml or /etc/chairlift/channels.yml; ChairLift's
	# built-in table applies when neither exists, so nothing is installed to
	# either of those paths here.
	install -Dm644 channels.example.yml $(DESTDIR)$(DATADIR)/doc/chairlift/channels.example.yml
	# Remove legacy passwordless rules from prior source installs
	rm -f $(DESTDIR)$(POLKITRULESDIR)/org.frostyard.ChairLift.bootc.rules
	rm -f $(DESTDIR)$(POLKITRULESDIR)/org.frostyard.ChairLift.updex.rules
	# Install PolicyKit policy for bootc
	install -Dm644 data/io.projectbluefin.chairlift.bootc.policy $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.bootc.policy
	# Install PolicyKit policy for updex
	install -Dm644 data/io.projectbluefin.chairlift.updex.policy $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.updex.policy
	# Install PolicyKit policy for native A/B sysupdate staging
	install -Dm644 data/io.projectbluefin.chairlift.sysupdate.policy $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.sysupdate.policy
	# Install PolicyKit policy for the Bluefin-family channel/developer helper
	install -Dm644 data/io.projectbluefin.chairlift.ublue.policy $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.ublue.policy

# Compile the Livery GSettings schema for a source build.
#
# `gsettings` reads schemas from $(DATADIR)/glib-2.0/schemas and from
# $$GSETTINGS_SCHEMA_DIR. A developer running build/chairlift has not run
# `make install`, so without this the Livery page correctly reports its schema
# missing. Run `make schemas` once, then export
# GSETTINGS_SCHEMA_DIR=$(CURDIR)/$(BUILD_DIR)/schemas.
# Skipped where glib-compile-schemas is absent, so a build host that only has
# a Go toolchain still builds; the application reports its schema missing and
# the Livery page degrades, which is the same path an uninstalled build takes.
#
# The whole recipe is one shell invocation because each recipe line otherwise
# gets its own shell: an `exit 0` guard on the first line would end that line
# alone and let the later glib-compile-schemas line run and fail with 127,
# which would break `make build` on the Go-only host this skip exists for.
schemas:
	@if ! command -v glib-compile-schemas >/dev/null 2>&1; then \
		echo "==> skipping schemas: glib-compile-schemas not installed"; \
	else \
		mkdir -p $(BUILD_DIR)/schemas && \
		cp data/io.projectbluefin.chairlift.livery.gschema.xml $(BUILD_DIR)/schemas/ && \
		cp data/io.projectbluefin.chairlift.updates.gschema.xml $(BUILD_DIR)/schemas/ && \
		glib-compile-schemas $(BUILD_DIR)/schemas && \
		echo "==> schemas compiled to $(BUILD_DIR)/schemas"; \
	fi

# Uninstall the application
uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY_NAME)
	rm -f $(DESTDIR)$(BINDIR)/chairlift-wrapper
	rm -f $(DESTDIR)$(APPLICATIONSDIR)/io.projectbluefin.chairlift.desktop
	rm -f $(DESTDIR)$(CONFIGDIR)/config.yml
	rm -f $(DESTDIR)$(SCHEMASDIR)/io.projectbluefin.chairlift.livery.gschema.xml
	rm -f $(DESTDIR)$(SCHEMASDIR)/io.projectbluefin.chairlift.updates.gschema.xml
	rm -f $(DESTDIR)$(ICONSDIR)/hicolor/scalable/apps/io.projectbluefin.chairlift.svg
	rm -f $(DESTDIR)$(ICONSDIR)/hicolor/symbolic/apps/io.projectbluefin.chairlift-symbolic.svg
	rm -f $(DESTDIR)$(BINDIR)/$(HELPER_NAME)
	rm -f $(DESTDIR)$(BINDIR)/$(UBLUE_HELPER_NAME)
	rm -f $(DESTDIR)$(DATADIR)/doc/chairlift/channels.example.yml
	rm -f $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.ublue.policy
	rm -f $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.bootc.policy
	rm -f $(DESTDIR)$(POLKITRULESDIR)/org.frostyard.ChairLift.bootc.rules
	rm -f $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.updex.policy
	rm -f $(DESTDIR)$(POLKITRULESDIR)/org.frostyard.ChairLift.updex.rules
	rm -f $(DESTDIR)$(POLKITACTIONSDIR)/io.projectbluefin.chairlift.sysupdate.policy

# One command mirrors CI's host-independent gates (verify → lint → unit → race
# → build), in fail-fast order. The GTK/Xvfb-dependent E2E job runs separately
# through `make e2e`. The mill's deep gate calls this target.
#
# The build step reproduces CI's GOOS/GOARCH matrix (linux/amd64 and
# linux/arm64) into per-arch subdirectories, then rebuilds natively so
# build/chairlift is left runnable on this host. A host-arch-only build would
# let an arm64 compile failure pass locally and break CI.
.PHONY: ci
ci:
	@echo "==> verify: go.mod is tidy"
	$(GOMOD) tidy
	git diff --exit-code go.mod go.sum
	@echo "==> verify: go vet"
	CGO_ENABLED=$(CGO_ENABLED) $(GOCMD) vet ./...
	@echo "==> verify: gofmt"
	test -z "$$(gofmt -l .)"
	@echo "==> lint"
	CGO_ENABLED=$(CGO_ENABLED) golangci-lint run
	@echo "==> unit tests"
	CGO_ENABLED=$(CGO_ENABLED) $(GOTEST) ./internal/... -run "^Test[^I]" -skip "Integration"
	@echo "==> race detector"
	CGO_ENABLED=1 $(GOTEST) -race -short ./internal/... -run "^Test[^I]" -skip "Integration"
	@echo "==> build"
	GOOS=linux GOARCH=amd64 $(MAKE) build BUILD_DIR=$(BUILD_DIR)/ci-linux-amd64
	GOOS=linux GOARCH=arm64 $(MAKE) build BUILD_DIR=$(BUILD_DIR)/ci-linux-arm64
	$(MAKE) build
	@echo "==> CI mirror passed"

bump: ## tag the next calendar version (YY.MM.N); PRE=alpha.1 for a prerelease
	@$(MAKE) build
	@$(MAKE) test
	@$(MAKE) fmt
	$(MAKE) lint
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Working directory is not clean. Please commit or stash changes before bumping version."; \
		exit 1; \
	fi
	@echo "Creating new tag..."
	@version=$$(./scripts/next-version.sh $(PRE)); \
		git tag -a $$version -m "Version $$version"; \
		echo "Tagged version $$version"; \
		echo "Pushing tag $$version to origin..."; \
		git push origin $$version

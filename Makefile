BINARY    := awssso
INSTALL   := /usr/local/bin/$(BINARY)
CHANGELOG := CHANGELOG.md

.PHONY: build install patch minor major check-version next-version

# ── Build & install ───────────────────────────────────────────────────────────

build:
	@go build -o $(BINARY) .
	@echo "✔ Built $(BINARY)"

install: build
	@cp $(BINARY) $(INSTALL)
	@echo "✔ Installed to $(INSTALL)"

test:
	@go test ./...
	@echo "✔ Tests passed"

# ── Version helpers ───────────────────────────────────────────────────────────

# Read the current major version from the latest changelog entry
CURRENT_MAJOR := $(shell grep -m1 '^## v' $(CHANGELOG) | sed 's/## v\([0-9]*\)\..*/\1/')

# Fibonacci sequence lookup: given N, return the next Fibonacci number
next-fib = $(shell \
	n=$(1); \
	a=1; b=1; \
	while [ "$$b" -le "$$n" ]; do t=$$b; b=$$((a+b)); a=$$t; done; \
	echo $$b)

NEXT_MAJOR := $(call next-fib,$(CURRENT_MAJOR))

# Current minor/patch from CHANGELOG
CURRENT_MINOR := $(shell grep -m1 '^## v' $(CHANGELOG) | sed 's/## v[0-9]*\.\([0-9]*\)\..*/\1/')
CURRENT_PATCH := $(shell grep -m1 '^## v' $(CHANGELOG) | sed 's/## v[0-9]*\.[0-9]*\.\([0-9]*\).*/\1/')

NEXT_MINOR_VER := $(CURRENT_MAJOR).$(shell echo $$(($(CURRENT_MINOR)+1))).0
NEXT_PATCH_VER := $(CURRENT_MAJOR).$(CURRENT_MINOR).$(shell echo $$(($(CURRENT_PATCH)+1)))
NEXT_MAJOR_VER := $(NEXT_MAJOR).0.0

DATE := $(shell date +%Y-%m-%d)

# ── Release targets ───────────────────────────────────────────────────────────

patch: test _bump-patch install
	@echo "✔ Released v$(NEXT_PATCH_VER)"

minor: test _bump-minor install
	@echo "✔ Released v$(NEXT_MINOR_VER)"

major: test _bump-major install
	@echo "✔ Released v$(NEXT_MAJOR_VER)"

_bump-patch:
	@$(MAKE) _bump-changelog VERSION=$(NEXT_PATCH_VER)

_bump-minor:
	@$(MAKE) _bump-changelog VERSION=$(NEXT_MINOR_VER)

_bump-major:
	@$(MAKE) _bump-changelog VERSION=$(NEXT_MAJOR_VER)

_bump-changelog:
	@if [ -z "$(VERSION)" ]; then echo "VERSION not set"; exit 1; fi
	@ENTRY="\n## v$(VERSION) — $(DATE)\n\n### Features\n- (describe what was added)\n\n### Fixes\n- (describe what was fixed)\n"; \
	 sed -i '' "s|^---$$|---$$ENTRY|" $(CHANGELOG); \
	 echo "✔ CHANGELOG updated — edit the v$(VERSION) entry then commit"

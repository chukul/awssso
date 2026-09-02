BINARY    := awssso
INSTALL   := /usr/local/bin/$(BINARY)
CHANGELOG := CHANGELOG.md

# Extract version by matching semver pattern — avoids # which Make treats as comments
VERSION   := $(shell grep -om1 'v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*' $(CHANGELOG))
LDFLAGS   := -ldflags "-X main.Version=$(VERSION)"

# Parse major/minor/patch individually using the same pattern
CUR_MAJOR := $(shell grep -om1 'v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*' $(CHANGELOG) | cut -d. -f1 | tr -d v)
CUR_MINOR := $(shell grep -om1 'v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*' $(CHANGELOG) | cut -d. -f2)
CUR_PATCH := $(shell grep -om1 'v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*' $(CHANGELOG) | cut -d. -f3)

NEXT_MINOR_VER := $(CUR_MAJOR).$(shell echo $$(($(CUR_MINOR)+1))).0
NEXT_PATCH_VER := $(CUR_MAJOR).$(CUR_MINOR).$(shell echo $$(($(CUR_PATCH)+1)))
DATE          := $(shell date +%Y-%m-%d)

.PHONY: build install test patch minor

# ── Build & install ───────────────────────────────────────────────────────────

build:
	@go build $(LDFLAGS) -o $(BINARY) .
	@echo "✔ Built $(BINARY) $(VERSION)"

install: build
	@cp $(BINARY) $(INSTALL)
	@echo "✔ Installed to $(INSTALL)"

test:
	@go test ./...
	@echo "✔ Tests passed"

# ── Release targets ───────────────────────────────────────────────────────────

patch: test
	@$(MAKE) _bump-changelog VER=$(NEXT_PATCH_VER)
	@$(MAKE) install

minor: test
	@$(MAKE) _bump-changelog VER=$(NEXT_MINOR_VER)
	@$(MAKE) install

_bump-changelog:
	@if [ -z "$(VER)" ]; then echo "VER not set"; exit 1; fi
	@ENTRY="\n## v$(VER) — $(DATE)\n\n### Features\n- (describe what was added)\n\n### Fixes\n- (describe what was fixed)\n"; \
	 sed -i '' "s|^---$$|---$$ENTRY|" $(CHANGELOG); \  # macOS/BSD sed; Linux: remove the ''
	 echo "✔ CHANGELOG updated — edit the v$(VER) entry then commit"

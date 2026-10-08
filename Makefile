GO ?= go
# gofmt comes from the same toolchain as $(GO) when $(GO) is a path, and from PATH
# otherwise: a formatting gate that silently checks nothing because the binary was not
# found is worse than no gate at all.
GOFMT ?= $(or $(wildcard $(dir $(GO))gofmt),gofmt)
.PHONY: build web test check browser-check browser-steps dev release format docker docker-check notices rehearsal rollback-rehearsal docker-inputs compose-env env-coverage source-package
web:
	cd web && npm ci && npm run build
build: web
	mkdir -p dist
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o dist/brrpbrrp ./cmd/brrpbrrp
test: web
	cd web && npm test
	# 60 minutes, not 40: `go test -race ./...` runs the packages in parallel, and the
	# API package alone took 2402s (just past a 40m budget) on this tree's first
	# measured sweep — a timeout that reports nothing about the code
	# (0.4.0-alpha.8, review 17 handoff).
	$(GO) test -race -count=1 -timeout 60m ./...
# The acceptance matrix is checked here rather than by reading it: a scenario with no
# evidence, a check that was renamed or deleted, and a plan id missing from the matrix all
# fail the build. The frontend unit tests for the checks themselves live in web/tests.
check: web
	@node scripts/check-acceptance-matrix.mjs
	$(GO) vet ./...
	@unformatted="$$($(GOFMT) -l internal cmd scripts)"; \
	if [ -n "$$unformatted" ]; then \
		echo "these files are not gofmt-canonical:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(GO) mod tidy -diff
	cd web && npm run check && npm run format:check
	BRRP_GO='$(GO)' node scripts/licenses.mjs --check
	node scripts/check-doc-links.mjs --self-test
	node scripts/check-doc-links.mjs
	node scripts/check-schema-docs.mjs --self-test
	node scripts/check-schema-docs.mjs
	node scripts/check-docker-inputs.mjs --self-test
	node scripts/check-docker-inputs.mjs
	node scripts/check-compose-env.mjs --self-test
	node scripts/check-compose-env.mjs
	node scripts/check-env-coverage.mjs --self-test
	node scripts/check-env-coverage.mjs
	node scripts/check-workflow-shells.mjs --self-test
	node scripts/check-workflow-shells.mjs
	BRRP_GO='$(GO)' node scripts/check-source-package.mjs
	node scripts/check-browser-steps.mjs --self-test
	node scripts/check-browser-steps.mjs

# The browser steps must fail on a failing suite rather than on tee's status. The
# audit executes those step bodies against a failing stub, so it is a real
# control rather than a reading of the YAML.
browser-steps:
	node scripts/check-browser-steps.mjs --self-test
	node scripts/check-browser-steps.mjs
docker-inputs:
	node scripts/check-docker-inputs.mjs --self-test
	node scripts/check-docker-inputs.mjs
compose-env:
	node scripts/check-compose-env.mjs --self-test
	node scripts/check-compose-env.mjs
env-coverage:
	node scripts/check-env-coverage.mjs --self-test
	node scripts/check-env-coverage.mjs
source-package:
	BRRP_GO='$(GO)' node scripts/check-source-package.mjs
notices:
	BRRP_GO='$(GO)' node scripts/licenses.mjs ./cmd/brrpbrrp $(if $(VERSION),--version $(VERSION),)

# Capacity qualification. Both halves need a running server, so neither is wired into
# `check`: a capacity run on a developer's laptop is a development measurement, and
# folding it into the ordinary gate would make the gate mean something different from
# what its name says.
#
#   make capacity-fixture BASE=http://127.0.0.1:8080 OWNER=you PASS=... \
#        ACCOUNTS=250 CHANNELS=100 MESSAGES=1000000 PRIVATE=1
#   make capacity-load    BASE=... COOKIE='brrp_session=...' CLIENTS=100 DURATION=7200
CAPACITY_BASE ?= http://127.0.0.1:8080
capacity-fixture:
	node scripts/capacity-fixture.mjs --base '$(CAPACITY_BASE)' \
		--owner-user '$(OWNER)' --owner-pass '$(PASS)' \
		--accounts $(or $(ACCOUNTS),250) --channels $(or $(CHANNELS),100) \
		--messages $(or $(MESSAGES),1000000) \
		$(if $(PRIVATE),--private-traffic,) \
		--report '$(or $(REPORT),planning/m5-checkpoint/fixture.json)' --label '$(or $(LABEL),make)'
capacity-load:
	node scripts/capacity-load.mjs --base '$(CAPACITY_BASE)' --cookie '$(COOKIE)' \
		--clients $(or $(CLIENTS),100) --duration $(or $(DURATION),7200) \
		--report '$(or $(REPORT),planning/m5-checkpoint/load.json)' --label '$(or $(LABEL),make)' 
# Runs the documented encrypted off-host backup procedure end to end on disposable data
# directories: back up, encrypt off the host, delete the plaintext, restore only from the
# encrypted copy, and prove the message and attachment bytes survived.
rehearsal-encrypted:
	$(GO) build -o dist/brrpbrrp-rehearsal ./cmd/brrpbrrp
	node scripts/backup-encrypted-rehearsal.mjs --binary dist/brrpbrrp-rehearsal

# The full-disk rehearsal: a real server on a filesystem of its own that really has no
# room, so the disk guard's refusal is a fact about a filesystem rather than a number
# injected into a function. Needs no root on macOS; reports a skip elsewhere.
rehearsal-full-disk:
	$(GO) build -o dist/brrpbrrp-rehearsal ./cmd/brrpbrrp
	node scripts/full-disk-rehearsal.mjs --binary dist/brrpbrrp-rehearsal

# An external probe for a deployment this repository does not control. Point it at the
# hostname a member uses, not at localhost: what it checks is the edge — the proxy, the
# certificate, the asset cache, the websocket upgrade.
probe:
	node scripts/probe.mjs --base '$(BASE)' $(if $(COOKIE),--cookie '$(COOKIE)',)
browser-check: build
	$(GO) build -o dist/brrpbrrp-e2e ./cmd/brrpbrrp
	cd web && npm run test:e2e
dev: web
	$(GO) run ./cmd/brrpbrrp serve
release: web
	BRRP_GO='$(GO)' $(GO) run scripts/release.go
format:
	$(GO) fmt ./...
	cd web && npm run format
docker:
	docker build -t brrpbrrp:local .
docker-check: docker
	node scripts/check-docker.mjs brrpbrrp:local
# Rehearses the cross-version path with a real archived prior release: install with the
# old executable, snapshot with it, upgrade with this tree, and roll back with the old
# executable's own archive. It needs an archived release for this platform under
# dist/releases, or --old-release/--old-binary (see docs/deployment.md).
rehearsal:
	node scripts/upgrade-recovery.mjs

# Rehearses fresh recovery of consented private state — requests, blocks, cursors,
# preferences, private attachment bytes, receipts and a removed participant — with the
# private gate explicitly opened on disposable data (review 8, R8-05).
private-rehearsal:
	node scripts/upgrade-recovery-private.mjs

rollback-rehearsal:
	node scripts/upgrade-rollback.mjs

# Upgrades a real schema-13 data directory with the current tree and proves every
# value and identity survived. The seed is written by the preserved baseline, whose
# hash and embedded revision are recorded in scripts/schema13-baselines.json — the
# rehearsal refuses any executable that does not match, and refuses a platform with no
# recorded identity rather than accepting a lookalike (review 17, V17-05).
SCHEMA13_BASELINE ?= dist/brrpbrrp-p9-checkpoint
SCHEMA13_SEED ?= /tmp/brrpbrrp-schema13-seed
SCHEMA13_REPORT_DIR ?= planning/schema13-rehearsal
schema13-rehearsal: build
	mkdir -p $(SCHEMA13_REPORT_DIR)
	node scripts/upgrade-schema13-private.mjs --make-seed $(SCHEMA13_SEED) \
		--old-binary $(SCHEMA13_BASELINE) \
		--report $(SCHEMA13_REPORT_DIR)/seed.json
	node scripts/upgrade-schema13-private.mjs --seed-dir $(SCHEMA13_SEED) \
		--binary dist/brrpbrrp \
		--report $(SCHEMA13_REPORT_DIR)/upgrade.json

GO ?= go
.PHONY: build web test check browser-check browser-steps dev release format docker docker-check notices rehearsal rollback-rehearsal docker-inputs source-package
web:
	cd web && npm ci && npm run build
build: web
	mkdir -p dist
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o dist/brrpbrrp ./cmd/brrpbrrp
test: web
	cd web && npm test
	$(GO) test -race -count=1 -timeout 40m ./...
check: web
	$(GO) vet ./...
	cd web && npm run check && npm run format:check
	BRRP_GO='$(GO)' node scripts/licenses.mjs --check
	node scripts/check-docker-inputs.mjs --self-test
	node scripts/check-docker-inputs.mjs
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
source-package:
	BRRP_GO='$(GO)' node scripts/check-source-package.mjs
notices:
	BRRP_GO='$(GO)' node scripts/licenses.mjs
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

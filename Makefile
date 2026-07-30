BIN := worktree
VERSION := v1
# One asset per platform key the catalog entry publishes: macOS is one universal build for
# every Mac (the entry's `macos` key), Linux is one build per architecture. This list is what
# a release bakes — the release workflow runs `dist` rather than enumerating platforms of its
# own, so the keys published and the keys built here cannot drift apart, and a platform is
# added in one place.
PLATFORMS := macos-universal linux-x64 linux-arm64

.PHONY: build test install dist clean

build:
	go build -o $(BIN) .

test:
	@test -z "$$(gofmt -l .)" || { echo "gofmt:"; gofmt -l .; exit 1; }
	go vet ./...
	go test ./...

# Hand-install into an amenbo base dir — the author's own loop, before there is a release
# to install from:
#
#   make install AMENBO_BASE="$$AMENBO_HOME"
#
# The real route is `amenbo plugin install worktree`, which resolves the catalog entry,
# fetches the released asset and verifies its provenance before laying it down. This
# target skips all of that, so point it at a throwaway base, never at the one holding
# work you care about.
install: build
ifndef AMENBO_BASE
	$(error set AMENBO_BASE to the amenbo base dir to install into)
endif
	mkdir -p "$(AMENBO_BASE)/plugins/$(BIN)"
	cp $(BIN) "$(AMENBO_BASE)/plugins/$(BIN)/$(BIN)"
	cp dev/manifest.json "$(AMENBO_BASE)/plugins/$(BIN)/manifest.json"
	@echo "installed into $(AMENBO_BASE)/plugins/$(BIN) — enable it with: amenbo plugin enable $(BIN)"

# The release build: the tarballs a release carries and the digests the catalog entry
# quotes. Run it, upload dist/*.tar.gz to the release, and copy the printed digests into
# the entry — a wrong one fails the install, since the digest is what install verifies.
#
# **The entry inside each tarball is the plugin's name**, flat: that is the file install
# lays down, and it is looked for by name.
dist: test
	rm -rf dist && mkdir -p dist/build
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/build/$(BIN)-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/build/$(BIN)-darwin-amd64 .
	lipo -create -output dist/build/$(BIN)-macos-universal dist/build/$(BIN)-darwin-arm64 dist/build/$(BIN)-darwin-amd64
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/build/$(BIN)-linux-x64 .
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/build/$(BIN)-linux-arm64 .
	@for p in $(PLATFORMS); do \
		mkdir -p dist/stage/$$p && cp dist/build/$(BIN)-$$p dist/stage/$$p/$(BIN) && chmod +x dist/stage/$$p/$(BIN); \
		tar -czf dist/$(BIN)-$(VERSION)-$$p.tar.gz -C dist/stage/$$p $(BIN); \
	done
	@echo; echo "assets for the catalog entry (checksum: sha256:<digest>):"; cd dist && shasum -a 256 *.tar.gz

clean:
	rm -f $(BIN)
	rm -rf dist

BIN := worktree

.PHONY: build test install clean

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

clean:
	rm -f $(BIN)

.PHONY: build clean test

CMD_BINARIES = rogue-pipeline rogue-coordinator
TOOL_BINARIES = rogue-store rogue-scheduler rogue-iam rogue-telegram

build: $(CMD_BINARIES) $(TOOL_BINARIES)

GO_SOURCES = $(shell find . -name '*.go' -not -path './docs/*')

rogue-pipeline rogue-coordinator: rogue-%: $(GO_SOURCES)
	go build -o $@ ./cmd/$@

rogue-store rogue-scheduler rogue-iam rogue-telegram: rogue-%: $(GO_SOURCES)
	go build -o $@ ./tools/$@

test:
	go test ./... -count=1

clean:
	rm -f $(CMD_BINARIES) $(TOOL_BINARIES)

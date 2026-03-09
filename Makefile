.PHONY: build clean test

CMD_BINARIES = rogue-pipeline rogue-coordinator
TOOL_BINARIES = rogue-store rogue-scheduler rogue-iam rogue-telegram

build: $(CMD_BINARIES) $(TOOL_BINARIES)

rogue-pipeline rogue-coordinator: rogue-%: cmd/rogue-%/main.go
	go build -o $@ ./cmd/$@

rogue-store rogue-scheduler rogue-iam rogue-telegram: rogue-%: tools/rogue-%/main.go
	go build -o $@ ./tools/$@

test:
	go test ./... -count=1

clean:
	rm -f $(CMD_BINARIES) $(TOOL_BINARIES)

.PHONY: build clean test

BINARIES = rogue-pipeline rogue-coordinator rogue-store rogue-scheduler rogue-iam

build: $(BINARIES)

rogue-%: cmd/rogue-%/main.go
	go build -o $@ ./cmd/$@

test:
	go test ./... -count=1

clean:
	rm -f $(BINARIES)

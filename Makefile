.PHONY: build run clean test

build:
	go build -o bin/rdb-analyzer cmd/main.go

run: build
	./bin/rdb-analyzer -input /data/dump.rdb -output /data/analysis.csv

clean:
	rm -rf bin/

test:
	go test -v ./...

install:
	go install ./cmd/rdb-analyzer
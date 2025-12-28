.PHONY: build run docker-build clean proto

IMAGE_NAME ?= mock-go-server
IMAGE_TAG ?= latest

proto:
	@if ! which protoc > /dev/null; then \
		echo "error: protoc not installed. Install from https://grpc.io/docs/protoc-installation/" >&2; \
		exit 1; \
	fi
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/mock.proto

build: proto
	go build -o bin/mock-server .

run:
	go run .

docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

clean:
	rm -rf bin/

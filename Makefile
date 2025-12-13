.PHONY: build run docker-build clean

IMAGE_NAME ?= mock-go-server
IMAGE_TAG ?= latest

build:
	go build -o bin/mock-server .

run:
	go run .

docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

clean:
	rm -rf bin/

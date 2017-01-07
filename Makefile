GLIDE	:= $(shell which glide)
GO=CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go
TAG=v1.3.0
BIN=route53-kubernetes
IMAGE=quay.io/molecule/$(BIN)

all: image
	docker push $(IMAGE):$(TAG)

build:
	$(GO) build -installsuffix cgo -o $(BIN) .

deps:
	$(GLIDE) install --strip-vendor --strip-vcs

image: build
	docker build -t $(IMAGE):$(TAG) .

.PHONY: clean

clean:
	rm $(BIN)

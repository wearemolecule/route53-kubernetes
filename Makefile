GO=CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go
TAG=v1.1.4
BIN=route53-kubernetes
IMAGE=quay.io/molecule/$(BIN)

all: image
	docker push $(IMAGE):$(TAG)

build:
	$(GO) build -a -installsuffix cgo -o $(BIN) .

image: build
	docker build -t $(IMAGE):$(TAG) .

.PHONY: clean

clean:
	rm $(BIN)

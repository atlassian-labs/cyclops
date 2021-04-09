VERSION = 1.4.0
IMAGE = cyclops:$(VERSION)

MANAGER_BIN = cyclops
CLI_BIN = kubectl-cycle
OBSERVER_BIN = observer

.PHONY: build-manager build-observer build-cli install-cli build docker build-manager-linux build-observer-linux build-cli-linux build-linux docker-save local srcclr
.DEFAULT_GOAL := build

install-cli:
	go build -o ${GOPATH}/bin/${CLI_BIN} -ldflags="-X main.version=${VERSION}" cmd/cli/main.go

build-observer:
	go build -o bin/${OBSERVER_BIN} -ldflags="-X main.version=${VERSION}" cmd/observer/main.go

build-manager:
	go build -o bin/${MANAGER_BIN} -ldflags="-X main.version=${VERSION}" cmd/manager/main.go

build-cli:
	go build -o bin/${CLI_BIN} -ldflags="-X main.version=${VERSION}" cmd/cli/main.go

build: build-manager build-cli build-observer

build-manager-linux:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/linux/${MANAGER_BIN} -ldflags="-X main.version=${VERSION}" cmd/manager/main.go

build-cli-linux:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/linux/${CLI_BIN} -ldflags="-X main.version=${VERSION}" cmd/cli/main.go

build-observer-linux:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/linux/${OBSERVER_BIN} -ldflags="-X main.version=${VERSION}" cmd/observer/main.go

build-linux: build-manager-linux build-cli-linux build-observer-linux

clean:
	rm -f bin/${MANAGER_BIN}
	rm -f bin/${CLI_BIN}
	rm -f bin/${OBSERVER_BIN}
	rm -f bin/linux/${MANAGER_BIN}
	rm -f bin/linux/${CLI_BIN}
	rm -f bin/linux/${OBSERVER_BIN}


test:
	go test -cover ./pkg/...
	go test -cover ./cmd/...

lint:
	golangci-lint run

docker:
	docker build -t $(IMAGE) .

# New version of operator-sdk no longer support generate CRDs directly
# Build archieved source code to support different arch
# Some variables in the operator-sdk gets value off git so there will be git error but this does not affect the compiling
install-operator-sdk:
	mkdir -p ${GOPATH}/src/github.com/operator-framework
	mkdir -p /tmp/operator-sdk/
	curl -fL https://github.com/operator-framework/operator-sdk/archive/refs/tags/v0.19.0.tar.gz -o /tmp/operator-sdk/v0.19.0.tar.gz
	-cd /tmp/operator-sdk/ && tar -xzf v0.19.0.tar.gz
	mv /tmp/operator-sdk/operator-sdk-0.19.0 ${GOPATH}/src/github.com/operator-framework/
	$(MAKE) -C ${GOPATH}/src/github.com/operator-framework/operator-sdk-0.19.0 tidy
	$(MAKE) -C ${GOPATH}/src/github.com/operator-framework/operator-sdk-0.19.0 install

# See https://sdk.operatorframework.io/docs/golang/quickstart/
generate-crds:
	mkdir -p build deploy/crds
	touch build/Dockerfile
	operator-sdk generate k8s
	operator-sdk generate crds --crd-version v1
	rm -rf build/

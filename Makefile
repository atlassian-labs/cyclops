VERSION = 1.10.4
# IMPORTANT! Update api version if a new release affects cnr
API_VERSION = 1.0.0
IMAGE = cyclops:$(VERSION)
ENVVAR ?= CGO_ENABLED=0
ARCH=$(if $(TARGETPLATFORM),$(lastword $(subst /, ,$(TARGETPLATFORM))),amd64)
BASE_PACKAGE = github.com/atlassian-labs/cyclops/pkg
CLI_BUILD_LD_FLAGS= -X 'main.version=${VERSION}' -X '${BASE_PACKAGE}/cli.apiVersion=${API_VERSION}'
OBSERVER_BUILD_LD_FLAGS = -X 'main.version=${VERSION}' -X '${BASE_PACKAGE}/observer.apiVersion=${API_VERSION}'
MANAGER_BUILD_LD_FLAGS = -X 'main.version=${VERSION}' -X '${BASE_PACKAGE}/controller/cyclenoderequest.apiVersion=${API_VERSION}'

MANAGER_BIN = cyclops
CLI_BIN = kubectl-cycle
OBSERVER_BIN = observer

LOCALBIN ?= $(CURDIR)/bin
CONTROLLER_GEN_VERSION = v0.14.0
CONTROLLER_GEN = $(LOCALBIN)/controller-gen
CONTROLLER_GEN_STAMP = $(LOCALBIN)/.controller-gen-$(CONTROLLER_GEN_VERSION)

.PHONY: build-manager build-observer build-cli install-cli build docker build-manager-linux build-observer-linux build-cli-linux build-linux docker-save local srcclr generate generate-crds generate-deepcopy controller-gen install-controller-gen
.DEFAULT_GOAL := build

install-cli:
	go build -o ${GOPATH}/bin/${CLI_BIN} -ldflags="${CLI_BUILD_LD_FLAGS}" cmd/cli/main.go

build-observer:
	go build -o bin/${OBSERVER_BIN} -ldflags="${OBSERVER_BUILD_LD_FLAGS}" cmd/observer/main.go

build-manager:
	go build -o bin/${MANAGER_BIN} -ldflags="${MANAGER_BUILD_LD_FLAGS}" cmd/manager/main.go

build-cli:
	go build -o bin/${CLI_BIN} -ldflags="${CLI_BUILD_LD_FLAGS}" cmd/cli/main.go

build: build-manager build-cli build-observer

build-manager-linux:
	$(ENVVAR) GOOS=linux GOARCH=$(ARCH) go build -a -installsuffix cgo -o bin/linux/${MANAGER_BIN} -ldflags="${MANAGER_BUILD_LD_FLAGS}" cmd/manager/main.go

build-cli-linux:
	$(ENVVAR) GOOS=linux GOARCH=$(ARCH) go build -a -installsuffix cgo -o bin/linux/${CLI_BIN} -ldflags="${CLI_BUILD_LD_FLAGS}" cmd/cli/main.go

build-observer-linux:
	$(ENVVAR) GOOS=linux GOARCH=$(ARCH) go build -a -installsuffix cgo -o bin/linux/${OBSERVER_BIN} -ldflags="${OBSERVER_BUILD_LD_FLAGS}" cmd/observer/main.go

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
	docker buildx build --build-arg ENVVAR="$(ENVVAR)" -t $(IMAGE) --platform linux/$(ARCH) .

# Generate CRDs and deepcopy functions using the pinned controller-gen version.
generate: generate-crds generate-deepcopy

controller-gen install-controller-gen: $(CONTROLLER_GEN) $(CONTROLLER_GEN_STAMP)

$(CONTROLLER_GEN) $(CONTROLLER_GEN_STAMP):
	mkdir -p $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
	@rm -f $(LOCALBIN)/.controller-gen-*
	@touch $(CONTROLLER_GEN_STAMP)

generate-crds: $(CONTROLLER_GEN) $(CONTROLLER_GEN_STAMP)
	mkdir -p deploy/crds
	$(CONTROLLER_GEN) crd paths="./pkg/apis/atlassian/v1/..." output:crd:dir=deploy/crds
	@# Rename to match existing _crd.yaml convention
	@for f in deploy/crds/atlassian.com_*.yaml; do \
		crd="$${f%.yaml}_crd.yaml"; \
		[ "$$f" != "$$crd" ] && [ ! "$$(echo $$f | grep _crd.yaml)" ] && mv "$$f" "$$crd" || true; \
	done

generate-deepcopy: $(CONTROLLER_GEN) $(CONTROLLER_GEN_STAMP)
	$(CONTROLLER_GEN) object paths="./pkg/apis/atlassian/v1/..."

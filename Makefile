VERSION ?= dev
REGISTRY ?= ghcr.io
OWNER ?= samyn92
BINARY := agent-tools

# ── CLI ──────────────────────────────────────────────────────────────
.PHONY: build test vet fmt

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/agent-tools

test:
	go test -v ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# ── MCP Servers ──────────────────────────────────────────────────────
SERVERS := $(wildcard servers/*)
SERVER_NAMES := $(notdir $(SERVERS))

.PHONY: build-server build-servers push-server push-servers

# Build a single server: make build-server SERVER=kubectl
build-server:
	@test -n "$(SERVER)" || (echo "usage: make build-server SERVER=<name>" && exit 1)
	@BIN_NAME=$(SERVER); \
	case "$(SERVER)" in kubectl|flux) BIN_NAME="mcp-$(SERVER)";; esac; \
	cd servers/$(SERVER) && \
		CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o dist/bin/$$BIN_NAME . && \
		cp manifest.json dist/
	@if [ "$(SERVER)" = "kubectl" ] && [ ! -f servers/kubectl/dist/bin/kubectl ]; then \
		echo "Bundling kubectl binary..."; \
		KUBE_VERSION=$$(curl -sL https://dl.k8s.io/release/stable.txt); \
		curl -sLo servers/kubectl/dist/bin/kubectl \
			"https://dl.k8s.io/release/$${KUBE_VERSION}/bin/linux/amd64/kubectl"; \
		chmod +x servers/kubectl/dist/bin/kubectl; \
	fi
	@if [ "$(SERVER)" = "flux" ] && [ ! -f servers/flux/dist/bin/flux ]; then \
		echo "Bundling flux binary..."; \
		FLUX_VERSION=$$(curl -sL https://api.github.com/repos/fluxcd/flux2/releases/latest | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'); \
		curl -sLo /tmp/flux.tar.gz \
			"https://github.com/fluxcd/flux2/releases/download/v$${FLUX_VERSION}/flux_$${FLUX_VERSION}_linux_amd64.tar.gz"; \
		tar -xzf /tmp/flux.tar.gz -C servers/flux/dist/bin/ flux; \
		chmod +x servers/flux/dist/bin/flux; \
	fi

# Build all servers
build-servers:
	@for name in $(SERVER_NAMES); do \
		echo "Building $$name..."; \
		$(MAKE) build-server SERVER=$$name; \
	done

# Push a single server: make push-server SERVER=kubernetes TAG=0.1.0
push-server: build
	@test -n "$(SERVER)" || (echo "usage: make push-server SERVER=<name> TAG=<version>" && exit 1)
	@test -n "$(TAG)" || (echo "usage: make push-server SERVER=<name> TAG=<version>" && exit 1)
	./$(BINARY) push servers/$(SERVER)/dist/ -t $(REGISTRY)/$(OWNER)/agent-tools/$(SERVER):$(TAG)

# Push all servers: make push-servers TAG=0.1.0
push-servers: build
	@test -n "$(TAG)" || (echo "usage: make push-servers TAG=<version>" && exit 1)
	@for name in $(SERVER_NAMES); do \
		echo "Pushing $$name..."; \
		$(MAKE) push-server SERVER=$$name TAG=$(TAG); \
	done

# ── Docker ───────────────────────────────────────────────────────────
.PHONY: docker-build

# Build Docker image for a server: make docker-build SERVER=kubernetes
docker-build:
	@test -n "$(SERVER)" || (echo "usage: make docker-build SERVER=<name>" && exit 1)
	docker build -t $(REGISTRY)/$(OWNER)/agent-tools/$(SERVER):$(VERSION) servers/$(SERVER)

# ── Housekeeping ─────────────────────────────────────────────────────
.PHONY: clean

clean:
	rm -f $(BINARY)
	@for dir in servers/*/dist; do \
		[ -d "$$dir" ] && rm -rf "$$dir" || true; \
	done

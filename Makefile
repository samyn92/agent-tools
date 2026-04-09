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

# Build a single server: make build-server SERVER=kubernetes
build-server:
	@test -n "$(SERVER)" || (echo "usage: make build-server SERVER=<name>" && exit 1)
	cd servers/$(SERVER) && \
		CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o dist/bin/$(SERVER) . && \
		cp manifest.json dist/

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

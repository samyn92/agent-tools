# Kubernetes MCP Tool — OCI artifact
# Builds a static Go binary that speaks MCP stdio protocol.
# Requires kubectl in the agent's container image.

FROM golang:1.26 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kubernetes .

FROM scratch
COPY --from=builder /app/kubernetes /bin/kubernetes
COPY manifest.json /manifest.json

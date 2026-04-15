FROM golang:1.26 AS builder
WORKDIR /app
COPY kubectl/go.mod kubectl/go.sum ./
COPY pkg/otelutil/ /pkg/otelutil/
RUN go mod download
COPY kubectl/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-kubectl .

FROM alpine:3.21
RUN apk add --no-cache kubectl
COPY --from=builder /app/mcp-kubectl /bin/mcp-kubectl
COPY kubectl/manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-kubectl"]

FROM golang:1.26 AS builder
WORKDIR /app
COPY helm/go.mod helm/go.sum ./
COPY pkg/mcputil/ /pkg/mcputil/
RUN go mod download
COPY helm/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-helm .

FROM alpine:3.21
RUN apk add --no-cache helm
COPY --from=builder /app/mcp-helm /bin/mcp-helm
COPY helm/manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-helm"]

FROM golang:1.26 AS builder
WORKDIR /app
COPY git/go.mod git/go.sum ./
COPY pkg/mcputil/ /pkg/mcputil/
RUN go mod download
COPY git/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-git .

FROM alpine:3.21
RUN apk add --no-cache git
COPY --from=builder /app/mcp-git /bin/mcp-git
COPY git/manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-git"]

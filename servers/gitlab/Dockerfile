FROM golang:1.26 AS builder
WORKDIR /app
COPY gitlab/go.mod gitlab/go.sum ./
COPY pkg/otelutil/ /pkg/otelutil/
RUN go mod download
COPY gitlab/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-gitlab .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/mcp-gitlab /bin/mcp-gitlab
COPY gitlab/manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-gitlab"]

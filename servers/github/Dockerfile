FROM golang:1.26 AS builder
WORKDIR /app
COPY github/go.mod github/go.sum ./
COPY pkg/otelutil/ /app/pkg/otelutil/
RUN go mod download
COPY github/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-github .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/mcp-github /bin/mcp-github
COPY github/manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-github"]

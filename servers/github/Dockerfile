FROM golang:1.26 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-github .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/mcp-github /bin/mcp-github
COPY manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-github"]

FROM golang:1.26 AS builder
WORKDIR /app
COPY kube-explore/go.mod kube-explore/go.sum ./
COPY pkg/mcputil/ /pkg/mcputil/
RUN go mod download
COPY kube-explore/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kube-explore .

FROM scratch
COPY --from=builder /app/kube-explore /bin/kube-explore
COPY kube-explore/manifest.json /manifest.json

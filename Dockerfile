# Multi-stage build for minimal production image
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build all binaries
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /node ./cmd/node/main.go
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /dashboard ./cmd/dashboard/main.go
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bats ./cmd/bats/main.go

# Minimal runtime
FROM alpine:3.19

RUN apk add --no-cache ca-certificates wget

WORKDIR /

COPY --from=builder /node /node
COPY --from=builder /dashboard /dashboard
COPY --from=builder /bats /bats

EXPOSE 8001 9000

ENTRYPOINT ["/node"]

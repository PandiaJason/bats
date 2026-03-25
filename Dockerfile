# Use a multi-stage build to keep the image small
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go.mod first and download dependencies
COPY go.mod ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the applications
RUN go build -o /node ./cmd/node/main.go
RUN go build -o /dashboard ./cmd/dashboard/main.go
RUN go build -o /bats ./cmd/bats/main.go

# Use a minimal alpine image for the final stage
FROM alpine:latest

WORKDIR /

# Copy the binaries from the builder stage
COPY --from=builder /node /node
COPY --from=builder /dashboard /dashboard
COPY --from=builder /bats /bats
COPY --from=builder /app/internal/dashboard/static /internal/dashboard/static
COPY --from=builder /app/certs /certs

# Expose the default port (will be mapped in compose)
EXPOSE 8000

# Run the node
ENTRYPOINT ["/node"]

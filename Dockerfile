# Use a minimal base image for the final build
FROM golang:1.23-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum (if present) first for caching dependencies
COPY go.mod ./
# COPY go.sum ./ # Uncomment if go.sum exists

# Download dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application statically linked
RUN CGO_ENABLED=0 GOOS=linux go build -o proxy cmd/ddos-proxy/main.go

# Use a scratch image or alpine for the smallest footprint
FROM alpine:latest

# Install ca-certificates, varnish and bash
RUN apk --no-cache add ca-certificates varnish bash

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/proxy .
COPY --from=builder /app/challenge.html .
COPY default.vcl /etc/varnish/default.vcl
COPY entrypoint.sh .

RUN chmod +x entrypoint.sh

# Expose the port
EXPOSE 8080

# Run the binary
CMD ["./entrypoint.sh"]

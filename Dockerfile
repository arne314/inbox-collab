FROM golang:1.25.4 AS builder
RUN apt-get update && \
    apt-get install -y libolm-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -v -o /usr/local/bin/app/ ./...

FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y libolm-dev ca-certificates && \
    apt-get clean

WORKDIR /app
COPY --from=builder /usr/local/bin/app/cmd /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]


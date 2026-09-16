# syntax=docker/dockerfile:1

FROM golang:1.27.0-bookworm AS dependencies
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM dependencies AS test
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*
COPY . .
CMD ["sh", "-c", "go test -count=1 ./... && go test -race -count=1 ./... && go vet ./..."]

FROM dependencies AS build
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gh-gateway ./cmd/gh-gateway

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates \
    && mkdir -p /certs
COPY --from=build /out/gh-gateway /usr/local/bin/gh-gateway
EXPOSE 8080 443 22
ENTRYPOINT ["/usr/local/bin/gh-gateway"]
CMD ["serve"]

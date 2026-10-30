# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE}" \
    -o /bin/nomore403 .

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates curl

WORKDIR /nomore403

COPY --from=builder /bin/nomore403 /usr/local/bin/nomore403
COPY payloads/ /nomore403/payloads/
RUN ln -s /nomore403/payloads /payloads

ENTRYPOINT ["nomore403"]

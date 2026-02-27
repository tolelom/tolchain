FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder
ARG TARGETARCH
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOARCH=$TARGETARCH go build -ldflags "-s -w" -o /tolchain-node ./cmd/node

FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl
RUN adduser -D -u 1000 tol
COPY --from=builder /tolchain-node /usr/local/bin/tolchain-node
USER tol
WORKDIR /home/tol
EXPOSE 8545 30303
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD curl -sf http://localhost:8545/ || exit 1
ENTRYPOINT ["tolchain-node"]

FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /tolchain-node ./cmd/node

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
RUN adduser -D -u 1000 tol
COPY --from=builder /tolchain-node /usr/local/bin/tolchain-node
USER tol
WORKDIR /home/tol
EXPOSE 8545 30303
ENTRYPOINT ["tolchain-node"]

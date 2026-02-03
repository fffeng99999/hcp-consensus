FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git make gcc musl-dev linux-headers

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN make build

FROM alpine:latest

RUN apk add --no-cache ca-certificates bash

COPY --from=builder /app/build/hcpd /usr/local/bin/

EXPOSE 26656 26657 1317 9090

CMD ["hcpd", "start"]

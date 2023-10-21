FROM golang:1.15.1 AS builder
WORKDIR /build
COPY . /build
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -v -o dns-sync

FROM alpine:3.10
COPY --from=builder /build/dns-sync /bin/dns-sync

ENTRYPOINT ["dns-sync"]

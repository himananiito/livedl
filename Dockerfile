FROM golang:1.11-alpine as builder

RUN apk add --no-cache \
        build-base \
        git && \
    go get github.com/gorilla/websocket && \
    go get golang.org/x/crypto/sha3 && \
    go get github.com/mattn/go-sqlite3 && \
    go get github.com/gin-gonic/gin

COPY . /tmp/livedl

RUN cd /tmp/livedl && \
    go build src/livedl.go



FROM alpine:3.8 

RUN apk add --no-cache \
        ca-certificates \
        ffmpeg \
        openssl

COPY --from=builder /tmp/livedl/livedl /usr/local/bin/

WORKDIR /livedl

VOLUME /livedl

CMD livedl


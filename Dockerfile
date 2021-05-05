FROM golang:1.16-alpine as builder

RUN apk add --no-cache \
        build-base \
        git

COPY . /tmp/livedl

RUN cd /tmp/livedl/src && \
    go build livedl.go



FROM alpine:3.8 

RUN apk add --no-cache \
        ca-certificates \
        ffmpeg \
        openssl

COPY --from=builder /tmp/livedl/src/livedl /usr/local/bin/

WORKDIR /livedl

VOLUME /livedl

ENTRYPOINT [ "livedl", "--no-chdir" ]

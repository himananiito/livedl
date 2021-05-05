FROM golang:1.16-alpine

RUN apk add mingw-w64-gcc

COPY ./src/ /livedl/src/

RUN \
    cd /livedl/src/ && \
    GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -o livedl.exe livedl.go

CMD cp /livedl/src/livedl.exe /mnt/

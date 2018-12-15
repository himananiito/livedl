
set-item env:GOARCH -value 386
set-item env:CGO_ENABLED -value 1

go build -o livedl.x86.exe src/livedl.go

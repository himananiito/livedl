package streamlink

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/himananiito/livedl/procs/base"
)

var cmdList = []string{
	"./bin/streamlink/streamlink",
	"./bin/Streamlink/Streamlink",
	"./bin/streamlink",
	"./bin/Streamlink",
	"./streamlink/streamlink",
	"./Streamlink/Streamlink",
	"./Streamlink",
	"streamlink",
	"Streamlink",
}

func Open(opt ...string) (cmd *exec.Cmd, stdout, stderr io.ReadCloser, err error) {
	cmd, _, stdout, stderr, err = base.Open(&cmdList, false, true, true, false, opt)
	if cmd == nil {
		err = fmt.Errorf("streamlink not found")
		return
	}
	return
}

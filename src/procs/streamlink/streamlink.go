package streamlink

import (
	"fmt"
	"io"
	"os/exec"
	"../base"
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

func Open(opt... string) (cmd *exec.Cmd, stderr io.ReadCloser, err error) {
	cmd, _, _, stderr, err = base.Open(&cmdList, false, false, true, true, opt)
	if cmd == nil {
		err = fmt.Errorf("streamlink not found")
		return
	}
	return
}

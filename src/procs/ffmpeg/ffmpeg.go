package ffmpeg

import (
	"fmt"
	"io"
	"os/exec"
	"../base"
)

var cmdList = []string{
	"./bin/ffmpeg/bin/ffmpeg",
	"./bin/ffmpeg/ffmpeg",
	"./bin/ffmpeg",
	"./ffmpeg/bin/ffmpeg",
	"./ffmpeg/ffmpeg",
	"./ffmpeg",
	"ffmpeg",
}

func Open(opt... string) (cmd *exec.Cmd, stdin io.WriteCloser, err error) {
	cmd, stdin, _, _, err = base.Open(&cmdList, true, false, false, true, opt)
	if cmd == nil {
		err = fmt.Errorf("ffmpeg not found")
		return
	}
	return
}

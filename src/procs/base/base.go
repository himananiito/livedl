package base

import (
	"io"
	"os"
	"os/exec"
)

func Open(cmdList *[]string, stdinEn, stdoutEn, stdErrEn, consoleEn bool, args []string) (cmd *exec.Cmd, stdin io.WriteCloser, stdout, stderr io.ReadCloser, err error) {

	for i, cmdName := range *cmdList {
		cmd = exec.Command(cmdName, args...)

		if stdinEn {
			stdin, err = cmd.StdinPipe()
			if err != nil {
				return
			}
		}

		if stdoutEn {
			stdout, err = cmd.StdoutPipe()
			if err != nil {
				return
			}
		} else {
			if consoleEn {
				cmd.Stdout = os.Stdout
			}
		}

		if stdErrEn {
			stderr, err = cmd.StderrPipe()
			if err != nil {
				return
			}
		} else {
			if consoleEn {
				cmd.Stderr = os.Stderr
			}
		}

		if err = cmd.Start(); err != nil {
			continue
		} else {
			if i != 0 {
				*cmdList = []string{cmdName}
			}
			//fmt.Printf("CMD: %#v\n", cmd.Args)
			return
		}
	}

	// prog not found
	cmd = nil
	return
}
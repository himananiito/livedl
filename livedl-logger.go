package main

import (
	"fmt"
	"bufio"
	"regexp"
	"sync"
	"os"
	"os/exec"
)

func main() {
	args := os.Args[1:]
	var vid string
	for _, s := range args {
		if ma := regexp.MustCompile(`(lv\d{9,})`).FindStringSubmatch(s); len(ma) > 0 {
			vid = ma[1]
		}
	}

	args = append(args, "-nicoDebug")
	cmd := exec.Command("livedl", args...)

	if vid == "" {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	} else {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			fmt.Println(err)
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			fmt.Println(err)
			return
		}

		name := fmt.Sprintf("log/%s.txt", vid)
		os.MkdirAll("log", os.ModePerm)
		f, err := os.Create(name)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer f.Close()

		var mtx sync.Mutex
		append := func(s string) {
			mtx.Lock()
			defer mtx.Unlock()
			f.WriteString(s)
		}

		go func() {
			rdr := bufio.NewReader(stdout)
			for {
				s, err := rdr.ReadString('\n')
				if err != nil {
					return
				}
				fmt.Print(s)
				append(s)
			}
			defer stdout.Close()
		}()
		go func() {
			rdr := bufio.NewReader(stderr)
			for {
				s, err := rdr.ReadString('\n')
				if err != nil {
					return
				}
				append(s)
			}
			defer stderr.Close()
		}()
		cmd.Run()
	}
}

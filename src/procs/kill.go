package procs

import (
	"fmt"
	"runtime"
	"log"

	"./base"
)

func Kill(pid int) {
	if runtime.GOOS == "windows" {
		options := []string{
			"/PID", fmt.Sprintf("%v", pid),
			"/T",
			"/F",
		}
		list := []string{"taskkill"}
		if taskkill, _, _, _, err := base.Open(&list, false, false, false, false, options); err == nil {
			taskkill.Wait()
		}

	} else {
		log.Fatalf("[FIXME] Kill for %v not supported", runtime.GOOS)
	}
}


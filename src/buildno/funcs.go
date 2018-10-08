package buildno

import (
	"fmt"
	"runtime"
)

func GetBuildNo() string {
	return fmt.Sprintf(
		"%v.%v-%s-%s",
		BuildDate,
		BuildNo,
		runtime.GOOS,
		runtime.GOARCH,
	)
}

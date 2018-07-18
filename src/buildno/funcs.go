package buildno

import (
	"fmt"
	"runtime"
)

func GetBuildNo() string {
	return fmt.Sprintf(
		"%v.%v-%s",
		BuildDate,
		BuildNo,
		runtime.GOOS,
	)
}

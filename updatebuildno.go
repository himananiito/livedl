package main

import (
	"log"
	"os"
	"io/ioutil"
	"time"
	"strconv"
	"regexp"
	"fmt"
)

func main() {
	f, err := os.OpenFile("src/buildno/buildno.go", os.O_RDWR, 0755)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if _, err := f.Seek(0, 0); err != nil {
		log.Fatal(err)
	}
	data, err := ioutil.ReadAll(f)
	if err != nil {
		log.Fatal(err)
	}

	var buildNo int64
	if ma := regexp.MustCompile(`BuildNo\s*=\s*"(\d+)"`).FindSubmatch(data); len(ma) > 0 {
		buildNo, err = strconv.ParseInt(string(ma[1]), 10, 64)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatal("BuildNo not match")
	}
	buildNo++

	var now = time.Now()
	buildDate := fmt.Sprintf("%04d%02d%02d",
		now.Year(),
		now.Month(),
		now.Day(),
	)

	fmt.Printf("%v.%v\n", buildDate, buildNo)

	if _, err := f.Seek(0, 0); err != nil {
		log.Fatal(err)
	}
	if err := f.Truncate(0); err != nil {
		log.Fatal(err)
	}

	f.WriteString(fmt.Sprintf(`
package buildno

var BuildDate = "%s"
var BuildNo = "%d"
`, buildDate, buildNo))

}

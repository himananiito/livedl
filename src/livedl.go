package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"./options"
	"./twitcas"
	"./niconico"
	"./youtube"
	"./zip2mp4"
)

func main() {
	var baseDir string
	if regexp.MustCompile(`\AC:\\.*\\Temp\\go-build[^\\]*\\[^\\]+\\exe\\[^\\]*\.exe\z`).MatchString(os.Args[0]) {
		// go runで起動時
		pwd, e := os.Getwd()
		if e != nil {
			fmt.Println(e)
			return
		}
		baseDir = pwd
	} else {
		//pa, e := filepath.Abs(os.Args[0])
		pa, e := os.Executable()
		if e != nil {
			fmt.Println(e)
			return
		}

		// symlinkを追跡する
		for {
			sl, e := os.Readlink(pa)
			if e != nil {
				break
			}
			pa = sl
		}
		baseDir = filepath.Dir(pa)
	}
	fmt.Printf("chdir: %s\n", baseDir)
	if e := os.Chdir(baseDir); e != nil {
		fmt.Println(e)
		return
	}

	opt := options.ParseArgs()

	switch opt.Command {
	default:
		fmt.Printf("Unknown command: %v\n", opt.Command)
		os.Exit(1)

	case "TWITCAS":
		twitcas.TwitcasRecord(opt.TcasId, "")

	case "YOUTUBE":
		youtube.Record(opt.YoutubeId)

	case "NICOLIVE":
		if err := niconico.Record(opt); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case "NICOLIVE_TEST":
		if err := niconico.TestRun(opt); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	case "ZIP2MP4":
		if err := zip2mp4.Convert(opt.ZipFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	case "DB2MP4":
		if err := zip2mp4.ConvertDB(opt.DBFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	}


	return
}

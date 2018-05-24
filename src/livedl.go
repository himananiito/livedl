package main

import (
	"fmt"
	"os"

	"./options"
	"./twitcas"
	"./niconico"
	"./youtube"
	"./zip2mp4"
)

func main() {

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

	}


	return
}

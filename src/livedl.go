package main

import (
	"fmt"
	"os"

	"./options"
	"./twitcas"
	"./niconico"
	"./youtube"
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
		youtube.Recoed(opt.YoutubeId)

	case "NICOLIVE":
		//if opt.ConfPass == "" {
		//	fmt.Println("-conf-pass <password> required")
		//	options.Help()
		//	return
		//}
		if err := niconico.Record(opt); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	return
}

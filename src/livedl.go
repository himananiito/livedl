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
	"time"
	"strings"
	"./httpbase"
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

	opt := options.ParseArgs()

	// chdir if not disabled
	if !opt.NoChdir {
		fmt.Printf("chdir: %s\n", baseDir)
		if e := os.Chdir(baseDir); e != nil {
			fmt.Println(e)
			return
		}
	}

	// http
	if opt.HttpRootCA != "" {
		if err := httpbase.SetRootCA(opt.HttpRootCA); err != nil {
			fmt.Println(err)
			return
		}
	}
	if opt.HttpSkipVerify {
		if err := httpbase.SetSkipVerify(true); err != nil {
			fmt.Println(err)
			return
		}
	}
	if opt.HttpProxy != "" {
		if err := httpbase.SetProxy(opt.HttpProxy); err != nil {
			fmt.Println(err)
			return
		}
	}

	switch opt.Command {
	default:
		fmt.Printf("Unknown command: %v\n", opt.Command)
		os.Exit(1)

	case "TWITCAS":
		var doneTime int64
		for {
			done, dbLocked := twitcas.TwitcasRecord(opt.TcasId, "")
			if dbLocked {
				break
			}
			if (! opt.TcasRetry) {
				break
			}

			if opt.TcasRetryTimeoutMinute < 0 {

			} else if done {
				doneTime = time.Now().Unix()

			} else {
				if doneTime == 0 {
					doneTime = time.Now().Unix()
				} else {
					delta := time.Now().Unix() - doneTime
					var minutes int
					if opt.TcasRetryTimeoutMinute == 0 {
						minutes = options.DefaultTcasRetryTimeoutMinute
					} else {
						minutes = opt.TcasRetryTimeoutMinute
					}

					if minutes > 0 {
						if delta > int64(minutes * 60) {
							break
						}
					}
				}
			}

			var interval int
			if opt.TcasRetryInterval <= 0 {
				interval = options.DefaultTcasRetryInterval
			} else {
				interval = opt.TcasRetryInterval
			}
			select {
			case <-time.After(time.Duration(interval) * time.Second):
			}
		}

	case "YOUTUBE":
		err := youtube.Record(opt.YoutubeId, opt.YtNoStreamlink, opt.YtNoYoutubeDl)
		if err != nil {
			fmt.Println(err)
		}

	case "NICOLIVE":
		hlsPlaylistEnd, dbname, err := niconico.Record(opt);
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if hlsPlaylistEnd && opt.NicoAutoConvert {
			done, nMp4s, err := zip2mp4.ConvertDB(dbname, opt.ConvExt, opt.NicoSkipHb)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if done {
				if nMp4s == 1 {
					if 1 <= opt.NicoAutoDeleteDBMode {
						os.Remove(dbname)
					}
				} else if 1 < nMp4s {
					if 2 <= opt.NicoAutoDeleteDBMode {
						os.Remove(dbname)
					}
				}
			}
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
		if strings.HasSuffix(opt.DBFile, ".yt.sqlite3") {
			zip2mp4.YtComment(opt.DBFile)

		} else if opt.ExtractChunks {
			if _, err := zip2mp4.ExtractChunks(opt.DBFile, opt.NicoSkipHb); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

		} else {
			if _, _, err := zip2mp4.ConvertDB(opt.DBFile, opt.ConvExt, opt.NicoSkipHb); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}
	}


	return
}

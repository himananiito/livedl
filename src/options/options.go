package options

import (
	"fmt"
	"regexp"
	"os"
	"strconv"
	"strings"
	"path/filepath"
	"io/ioutil"
	"../buildno"
)

type Option struct {
	Command string
	NicoLiveId string
	NicoStatusHTTPS bool
	NicoSession string
	NicoLoginId string
	NicoLoginPass string
	NicoRtmpMaxConn int
	NicoRtmpOnly bool
	NicoRtmpIndex map[int]bool
	NicoHlsOnly bool
	TcasId string
	YoutubeId string
	ConfFile string
	ConfPass string
}
func getCmd() (cmd string) {
	cmd = filepath.Base(os.Args[0])
	ext := filepath.Ext(cmd)
	cmd = strings.TrimSuffix(cmd, ext)
	return
}

func Help() {
	cmd := filepath.Base(os.Args[0])
	ext := filepath.Ext(cmd)
	cmd = strings.TrimSuffix(cmd, ext)

	format := `%s (%s.%s)
Usage:
%s [COMMAND] options... [--] FILE

COMMAND:
  -nico    ニコニコ生放送の録画
  -tcas    ツイキャスの録画
  -yt      YouTube Liveの録画

オプション/option:
  -conf-pass <password> 設定ファイルのパスワード
  -h                    ヘルプを表示
  --                    後にオプションが無いことを指定

オプション/option (ニコニコ生放送/nicolive):
  -nico-login <id>,<password>    ニコニコのIDとパスワードを設定し設定ファイルに書き込む
  -nico-session <session>        Cookie[user_session]を設定し設定ファイルに書き込む
  -nico-hls-only                 録画時にHLSのみを試す
  -nico-rtmp-only                録画時にRTMPのみを試す
  -nico-rtmp-max-conn <num>      RTMPの同時接続数を設定
  -nico-rtmp-index <num>[,<num>] RTMP録画を行うメディアファイルの番号を指定
  -nico-status-https             [実験的] getplayerstatusの取得にhttpsを使用する

FILE:
  ニコニコ生放送/nicolive:
    http://live2.nicovideo.jp/watch/lvXXXXXXXXX
    lvXXXXXXXXX
  ツイキャス/twitcasting:
    https://twitcasting.tv/XXXXX
    XXXX
`
	fmt.Printf(format, cmd, buildno.BuildDate, buildno.BuildNo, cmd)
	os.Exit(0)
}
func ParseArgs() (opt Option) {

	args := os.Args[1:]
	var match []string

	type Parser struct {
		re *regexp.Regexp
		cb func() error
	}

	nextArg := func() (str string, err error) {
		if len(args) <= 0 {
			if len(match[0]) > 0 {
				err = fmt.Errorf("%v: value required", match[0])
			} else {
				err = fmt.Errorf("value required")
			}
		} else {
			str = args[0]
			args = args[1:]
		}

		return
	}

	parseList := []Parser{
		Parser{regexp.MustCompile(`\A(?i)[-/](?:\?|h|help)\z`), func() error {
			Help()
			return nil
		}},
		Parser{regexp.MustCompile(`\A(https?://(?:[^/]*@)?(?:[^/]*\.)*nicovideo\.jp(?::[^/]*)?/(?:[^/]*?/)*)?(lv\d+)(?:\?.*)?\z`), func() error {
			switch opt.Command {
				default:
					fmt.Printf("Use \"--\" option for FILE for %s\n", opt.Command)
					Help()
				case "", "NICOLIVE":
					opt.NicoLiveId = match[2]
					opt.Command = "NICOLIVE"
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A--?conf-?pass\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}
			opt.ConfPass = str
			return
		}},
		Parser{regexp.MustCompile(`\Ahttps?://twitcasting\.tv/(\S+)\z`), func() error {
			opt.TcasId = match[1]
			opt.Command = "TWITCAS"
			return nil
		}},
		Parser{regexp.MustCompile(`\Ahttps?://(?:[^/]*\.)*youtube\.com/(?:.*\W)?v=([\w-]+)(?:[^\w-].*)?\z`), func() error {
			opt.YoutubeId = match[1]
			opt.Command = "YOUTUBE"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico\z`), func() error {
			opt.Command = "NICOLIVE"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?tcas\z`), func() error {
			opt.Command = "TWITCAS"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?(?:yt|youtube|youtube-live)\z`), func() error {
			opt.Command = "YOUTUBE"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?hls-?only\z`), func() error {
			opt.NicoHlsOnly = true
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?rtmp-?only\z`), func() error {
			opt.NicoRtmpOnly = true
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?rtmp-?index\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}
			ar := strings.Split(str, ",")
			if len(ar) > 0 {
				opt.NicoRtmpIndex = make(map[int]bool)
			}
			for _, s := range ar {
				num, err := strconv.Atoi(s)
				if err != nil {
					return fmt.Errorf("--nico-rtmp-index: Not a number: %s\n", s)
				}
				if num <= 0 {
					return fmt.Errorf("--nico-rtmp-index: Invalid: %d: must be greater than or equal to 1\n", num)
				}
				opt.NicoRtmpIndex[num-1] = true
			}
			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?status-?https\z`), func() error {
			// experimental
			opt.NicoStatusHTTPS = true
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?login\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}
			ar := strings.SplitN(str, ",", 2)
			if len(ar) >= 2 {
				opt.NicoLoginId = ar[0]
				opt.NicoLoginPass = ar[1]
			} else {
				return fmt.Errorf("--nico-login: <id>,<password>")
			}
			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?session\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}
			opt.NicoSession = str
			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?load-?session\z`), func() (err error) {
			name, err := nextArg()
			if err != nil {
				return
			}
			b, err := ioutil.ReadFile(name)
			if err != nil {
				return
			}
			if ma := regexp.MustCompile(`(\S+)`).FindSubmatch(b); len(ma) > 0 {
				opt.NicoSession = string(ma[1])
			} else {
				err = fmt.Errorf("--nico-load-session: load failured")
			}

			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?rtmp-?max-?conn\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}

			num, err := strconv.Atoi(str)
			if err != nil {
				return fmt.Errorf("--nico-rtmp-max-conn %v: %v", str, err)
			}
			opt.NicoRtmpMaxConn = num
			return
		}},
	}

	checkFILE := func(arg string) bool {
		switch opt.Command {
		default:
			//fmt.Printf("command not specified: -- \"%s\"\n", arg)
			//os.Exit(1)
		case "YOUTUBE":
			if ma := regexp.MustCompile(`v=([\w-]+)`).FindStringSubmatch(arg); len(ma) > 0 {
				opt.YoutubeId = ma[1]
				return true
			} else if ma := regexp.MustCompile(`\A([\w-]+)\z`).FindStringSubmatch(arg); len(ma) > 0 {
				opt.YoutubeId = ma[1]
				return true
			} else {
				fmt.Printf("Not YouTube id: %s\n", arg)
				os.Exit(1)
			}
		case "NICOLIVE":
			if ma := regexp.MustCompile(`(lv\d+)`).FindStringSubmatch(arg); len(ma) > 0 {
				opt.NicoLiveId = ma[1]
				return true
			}
		case "TWITCAS":
			if ma := regexp.MustCompile(`(?:.*/)?([^/]+)\z`).FindStringSubmatch(arg); len(ma) > 0 {
				opt.TcasId = ma[1]
				return true
			}
		}
		return false
	}

	LB_ARG: for len(args) > 0 {
		arg, _ := nextArg()

		if arg == "--" {
			switch len(args) {
			case 0:
				fmt.Printf("argument not specified after \"--\"\n")
				os.Exit(1)
			default:
				fmt.Printf("too many arguments after \"--\": %v\n", args)
				os.Exit(1)
			case 1:
				arg, _ := nextArg()
				checkFILE(arg)
			}

		} else {
			for _, p := range parseList {
				if match = p.re.FindStringSubmatch(arg); len(match) > 0 {
					if e := p.cb(); e != nil {
						fmt.Println(e)
						os.Exit(1)
					}
					continue LB_ARG
				}
			}
			if ok := checkFILE(arg); ! ok {
				fmt.Printf("Unknown option: %v\n", arg)
				Help()
			}
		}
	}

	if opt.ConfFile == "" {
		opt.ConfFile = fmt.Sprintf("%s.conf", getCmd())
	}

	switch opt.Command {
	case "":
		fmt.Printf("Command not specified\n")
		Help()
	case "YOUTUBE":
		if opt.YoutubeId == "" {
			Help()
		}
	case "NICOLIVE":
		if opt.NicoLiveId == "" {
			Help()
		}
	case "TWITCAS":
		if opt.TcasId == "" {
			Help()
		}
	default:
		fmt.Printf("[FIXME] argcheck for %s\n", opt.Command)
		os.Exit(1)
	}

	return
}

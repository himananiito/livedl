package options

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"../buildno"
	"../cryptoconf"
	"../files"
	"golang.org/x/crypto/sha3"
)

var DefaultTcasRetryTimeoutMinute = 5 // TcasRetryTimeoutMinute
var DefaultTcasRetryInterval = 60     // TcasRetryInterval

type Option struct {
	Command                string
	NicoLiveId             string
	NicoStatusHTTPS        bool
	NicoSession            string
	NicoLoginAlias         string
	NicoRtmpMaxConn        int
	NicoRtmpOnly           bool
	NicoRtmpIndex          map[int]bool
	NicoHlsOnly            bool
	NicoLoginOnly          bool
	NicoTestTimeout        int
	TcasId                 string
	TcasRetry              bool
	TcasRetryTimeoutMinute int // 再試行を終了する時間(初回終了または録画終了からの時間「分」)
	TcasRetryInterval      int // 再試行を行うまでの待ち時間
	YoutubeId              string
	ConfFile               string // deprecated
	ConfPass               string // deprecated
	ZipFile                string
	DBFile                 string
	NicoHlsPort            int
	NicoLimitBw            int
	NicoTsStart            float64
	NicoFormat             string
	NicoFastTs             bool
	NicoUltraFastTs        bool
	NicoAutoConvert        bool
	NicoAutoDeleteDBMode   int  // 0:削除しない 1:mp4が分割されなかったら削除 2:分割されても削除
	NicoDebug              bool // デバッグ情報の記録
	ConvExt                string
	ExtractChunks          bool
	NicoForceResv          bool // 終了番組の上書きタイムシフト予約
	YtNoStreamlink         bool
	YtNoYoutubeDl          bool
	NicoSkipHb             bool // コメント出力時に/hbコマンドを出さない
	HttpRootCA             string
	HttpSkipVerify         bool
	HttpProxy              string
	NoChdir                bool
}

func getCmd() (cmd string) {
	cmd = filepath.Base(os.Args[0])
	ext := filepath.Ext(cmd)
	cmd = strings.TrimSuffix(cmd, ext)
	return
}
func versionStr() string {
	cmd := filepath.Base(os.Args[0])
	ext := filepath.Ext(cmd)
	cmd = strings.TrimSuffix(cmd, ext)
	return fmt.Sprintf(`%s (%s)`, cmd, buildno.GetBuildNo())
}
func version() {
	fmt.Println(versionStr())
	os.Exit(0)
}
func Help(verbose ...bool) {
	cmd := filepath.Base(os.Args[0])
	ext := filepath.Ext(cmd)
	cmd = strings.TrimSuffix(cmd, ext)

	format := `%s (%s)
Usage:
%s [COMMAND] options... [--] FILE

COMMAND:
  -nico    ニコニコ生放送の録画
  -tcas    ツイキャスの録画
  -yt      YouTube Liveの録画
  -d2m     録画済みのdb(.sqlite3)をmp4に変換する(-db-to-mp4)

オプション/option:
  -h         ヘルプを表示
  -vh        全てのオプションを表示
  -v         バージョンを表示
  -no-chdir  起動する時chdirしない
  --         後にオプションが無いことを指定

ニコニコ生放送録画用オプション:
  -nico-login <id>,<password>    (+) ニコニコのIDとパスワードを指定する
  -nico-session <session>        Cookie[user_session]を指定する
  -nico-login-only=on            (+) 必ずログイン状態で録画する
  -nico-login-only=off           (+) 非ログインでも録画可能とする(デフォルト)
  -nico-hls-only                 録画時にHLSのみを試す
  -nico-hls-only=on              (+) 上記を有効に設定
  -nico-hls-only=off             (+) 上記を無効に設定(デフォルト)
  -nico-rtmp-only                録画時にRTMPのみを試す
  -nico-rtmp-only=on             (+) 上記を有効に設定
  -nico-rtmp-only=off            (+) 上記を無効に設定(デフォルト)
  -nico-rtmp-max-conn <num>      RTMPの同時接続数を設定
  -nico-rtmp-index <num>[,<num>] RTMP録画を行うメディアファイルの番号を指定
  -nico-hls-port <portnum>       [実験的] ローカルなHLSサーバのポート番号
  -nico-limit-bw <bandwidth>     (+) HLSのBANDWIDTHの上限値を指定する。0=制限なし
  -nico-format "FORMAT"          (+) 保存時のファイル名を指定する
  -nico-fast-ts                  倍速タイムシフト録画を行う(新配信タイムシフト)
  -nico-fast-ts=on               (+) 上記を有効に設定
  -nico-fast-ts=off              (+) 上記を無効に設定(デフォルト)
  -nico-auto-convert=on          (+) 録画終了後自動的にMP4に変換するように設定
  -nico-auto-convert=off         (+) 上記を無効に設定
  -nico-auto-delete-mode 0       (+) 自動変換後にデータベースファイルを削除しないように設定(デフォルト)
  -nico-auto-delete-mode 1       (+) 自動変換でMP4が分割されなかった場合のみ削除するように設定
  -nico-auto-delete-mode 2       (+) 自動変換でMP4が分割されても削除するように設定
  -nico-force-reservation=on     (+) 視聴にタイムシフト予約が必要な場合に自動的に上書きする
  -nico-force-reservation=off    (+) 自動的にタイムシフト予約しない(デフォルト)
  -nico-skip-hb=on               (+) コメント書き出し時に/hbコマンドを出さない
  -nico-skip-hb=off              (+) コメント書き出し時に/hbコマンドも出す(デフォルト)
  -nico-ts-start <num>           タイムシフトの録画を指定した再生時間(秒)から開始する
  -nico-ts-start-min <num>       タイムシフトの録画を指定した再生時間(分)から開始する

ツイキャス録画用オプション:
  -tcas-retry=on                 (+) 録画終了後に再試行を行う
  -tcas-retry=off                (+) 録画終了後に再試行を行わない
  -tcas-retry-timeout            (+) 再試行を開始してから終了するまでの時間（分)
                                     -1で無限ループ。デフォルト: 5分
  -tcas-retry-interval           (+) 再試行を行う間隔（秒）デフォルト: 60秒

Youtube live録画用オプション:
  -yt-api-key <key>              (+) YouTube Data API v3 keyを設定する(未使用)
  -yt-no-streamlink=on           (+) Streamlinkを使用しない
  -yt-no-streamlink=off          (+) Streamlinkを使用する(デフォルト)
  -yt-no-youtube-dl=on           (+) youtube-dlを使用しない
  -yt-no-youtube-dl=off          (+) youtube-dlを使用する(デフォルト)

変換オプション:
  -extract-chunks=off            (+) -d2mで動画ファイルに書き出す(デフォルト)
  -extract-chunks=on             (+) [上級者向] 各々のフラグメントを書き出す(大量のファイルが生成される)
  -conv-ext=mp4                  (+) -d2mで出力の拡張子を.mp4とする(デフォルト)
  -conv-ext=ts                   (+) -d2mで出力の拡張子を.tsとする

HTTP関連
  -http-skip-verify=on           (+) TLS証明書の認証をスキップする (32bit版対策)
  -http-skip-verify=off          (+) TLS証明書の認証をスキップしない (デフォルト)


(+)のついたオプションは、次回も同じ設定が使用されることを示す。

FILE:
  ニコニコ生放送/nicolive:
    http://live2.nicovideo.jp/watch/lvXXXXXXXXX
    lvXXXXXXXXX
  ツイキャス/twitcasting:
    https://twitcasting.tv/XXXXX
`
	fmt.Printf(format, cmd, buildno.GetBuildNo(), cmd)

	for _, b := range verbose {
		if b {
			fmt.Print(`
旧オプション:
  -conf-pass <password> [廃止] 設定ファイルのパスワード
  -z2m                  録画済みのzipをmp4に変換する(-zip-to-mp4)
  -nico-status-https    -

デバッグ用オプション:
  -nico-test-run           ニコ生テストラン
  -nico-test-timeout <num> ニコ生テストランでの各放送のタイムアウト
  -nico-test-format        フォーマット、保存しない
  -nico-ufast-ts           TS保存にウェイトを入れない
  -nico-debug              デバッグ用ログ出力する

HTTP関連
  -http-root-ca <file>    ルート証明書ファイルを指定(pem/der)
  -http-skip-verify       TLS証明書の認証をスキップする
  -http-proxy <proxy url> [警告] proxyを設定する
[警告] 情報流出に注意。信頼できるproxy serverのみに使用すること。

`)
			break
		}
	}

	os.Exit(0)
}

func dbConfSet(db *sql.DB, k string, v interface{}) {
	query := `INSERT OR REPLACE INTO conf (k,v) VALUES (?,?)`

	if _, err := db.Exec(query, k, v); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func SetNicoLogin(hash, user, pass string) (err error) {
	db, err := dbAccountOpen()
	if err != nil {
		if db != nil {
			db.Close()
		}
		return
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT OR IGNORE INTO niconico (alias, user, pass) VALUES(?, ?, ?);
		UPDATE niconico SET user = ?, pass = ? WHERE alias = ?
	`, hash, user, pass, user, pass, hash)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("niconico account saved.\n")
	return
}
func SetNicoSession(hash, session string) (err error) {
	db, err := dbAccountOpen()
	if err != nil {
		if db != nil {
			db.Close()
		}
		return
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT OR IGNORE INTO niconico (alias, session) VALUES(?, ?);
		UPDATE niconico SET session = ? WHERE alias = ?
	`, hash, session, session, hash)
	if err != nil {
		fmt.Println(err)
		return
	}
	return
}
func LoadNicoAccount(alias string) (user, pass, session string, err error) {
	db, err := dbAccountOpen()
	if err != nil {
		if db != nil {
			db.Close()
		}
		return
	}
	defer db.Close()

	db.QueryRow(`SELECT user, pass, IFNULL(session, "") FROM niconico WHERE alias = ?`, alias).Scan(&user, &pass, &session)
	return
}
func SetYoutubeApiKey(key string) (err error) {
	db, err := dbAccountOpen()
	if err != nil {
		if db != nil {
			db.Close()
		}
		return
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT OR IGNORE INTO youtubeapikey (id, key) VALUES(1, ?);
		UPDATE youtubeapikey SET key = ? WHERE id = 1
	`, key, key)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Youtube API KEY saved.\n")
	return
}
func LoadYoutubeApiKey() (key string, err error) {
	db, err := dbAccountOpen()
	if err != nil {
		if db != nil {
			db.Close()
		}
		return
	}
	defer db.Close()

	db.QueryRow(`SELECT IFNULL(key, "") FROM youtubeapikey WHERE id = 1`).Scan(&key)
	if key == "" {
		err = fmt.Errorf("apikey not found")
	}
	return
}
func dbAccountOpen() (db *sql.DB, err error) {

	base := func() string {
		if b := os.Getenv("LIVEDL_DIR"); b != "" {
			return b
		}
		if b := os.Getenv("APPDATA"); b != "" {
			return fmt.Sprintf("%s/livedl", b)
		}
		if b := os.Getenv("HOME"); b != "" {
			return fmt.Sprintf("%s/.livedl", b)
		}
		return ""
	}()
	if base == "" {
		log.Fatalln("basedir for account not defined")
	}

	name := fmt.Sprintf("%s/account.db", base)
	files.MkdirByFileName(name)
	db, err = sql.Open("sqlite3", name)
	if err != nil {
		log.Println(err)
		return
	}

	// niconico
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS niconico (
		alias TEXT PRIMARY KEY NOT NULL UNIQUE,
		user TEXT NOT NULL,
		pass TEXT NOT NULL,
		session TEXT
	)
	`)
	if err != nil {
		return
	}

	_, err = db.Exec(`
	CREATE UNIQUE INDEX IF NOT EXISTS niconico0 ON niconico(alias);
	CREATE UNIQUE INDEX IF NOT EXISTS niconico1 ON niconico(user);
	`)
	if err != nil {
		return
	}

	// youtube API key
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS youtubeapikey (
		id PRIMARY KEY NOT NULL UNIQUE,
		key TEXT
	)
	`)
	if err != nil {
		return
	}

	_, err = db.Exec(`
	CREATE UNIQUE INDEX IF NOT EXISTS youtubeapikey0 ON youtubeapikey(id);
	`)
	if err != nil {
		return
	}

	return
}

func dbOpen() (db *sql.DB, err error) {
	db, err = sql.Open("sqlite3", "conf.db")
	if err != nil {
		return
	}

	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS conf (
		k TEXT PRIMARY KEY NOT NULL UNIQUE,
		v BLOB
	)
	`)
	if err != nil {
		return
	}

	_, err = db.Exec(`
	CREATE UNIQUE INDEX IF NOT EXISTS conf0 ON conf(k);
	`)
	if err != nil {
		return
	}
	return
}

func ParseArgs() (opt Option) {
	//dbAccountOpen()
	db, err := dbOpen()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	defer db.Close()

	err = db.QueryRow(`
		SELECT
		IFNULL((SELECT v FROM conf WHERE k == "NicoFormat"), ""),
		IFNULL((SELECT v FROM conf WHERE k == "NicoLimitBw"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoLoginOnly"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoHlsOnly"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoRtmpOnly"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoFastTs"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoLoginAlias"), ""),
		IFNULL((SELECT v FROM conf WHERE k == "NicoAutoConvert"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoAutoDeleteDBMode"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "TcasRetry"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "TcasRetryTimeoutMinute"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "TcasRetryInterval"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "ConvExt"), ""),
		IFNULL((SELECT v FROM conf WHERE k == "ExtractChunks"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoForceResv"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "YtNoStreamlink"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "YtNoYoutubeDl"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "NicoSkipHb"), 0),
		IFNULL((SELECT v FROM conf WHERE k == "HttpSkipVerify"), 0);
	`).Scan(
		&opt.NicoFormat,
		&opt.NicoLimitBw,
		&opt.NicoLoginOnly,
		&opt.NicoHlsOnly,
		&opt.NicoRtmpOnly,
		&opt.NicoFastTs,
		&opt.NicoLoginAlias,
		&opt.NicoAutoConvert,
		&opt.NicoAutoDeleteDBMode,
		&opt.TcasRetry,
		&opt.TcasRetryTimeoutMinute,
		&opt.TcasRetryInterval,
		&opt.ConvExt,
		&opt.ExtractChunks,
		&opt.NicoForceResv,
		&opt.YtNoStreamlink,
		&opt.YtNoYoutubeDl,
		&opt.NicoSkipHb,
		&opt.HttpSkipVerify,
	)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

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
		Parser{regexp.MustCompile(`\A(?i)(?:--?|/)(?:\?|h|help)\z`), func() error {
			Help()
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)(?:--?|/)v(?:\?|h|help)\z`), func() error {
			Help(true)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?(?:v|version)\z`), func() error {
			version()
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
			case "NICOLIVE_TEST":
				opt.NicoLiveId = match[2]
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
		Parser{regexp.MustCompile(`\Ahttps?://twitcasting\.tv/([^/]+)(?:/.*)?\z`), func() error {
			opt.TcasId = match[1]
			opt.Command = "TWITCAS"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?tcas-?retry(?:=(on|off))\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.TcasRetry = true
			} else if strings.EqualFold(match[1], "off") {
				opt.TcasRetry = false
			}
			dbConfSet(db, "TcasRetry", opt.TcasRetry)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?tcas-?retry-?timeout(?:-?minutes?)?\z`), func() error {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--tcas-retry-timeout: Not a number: %s\n", s)
			}
			opt.TcasRetryTimeoutMinute = num
			dbConfSet(db, "TcasRetryTimeoutMinute", opt.TcasRetryTimeoutMinute)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?tcas-?retry-?interval\z`), func() error {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--tcas-retry-interval: Not a number: %s\n", s)
			}
			if num <= 0 {
				return fmt.Errorf("--tcas-retry-interval: Invalid: %d: greater than 1\n", num)
			}

			opt.TcasRetryInterval = num
			dbConfSet(db, "TcasRetryInterval", opt.TcasRetryInterval)
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
		Parser{regexp.MustCompile(`\A(?i)--?nico-?test-?run\z`), func() error {
			opt.Command = "NICOLIVE_TEST"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?test-?timeout\z`), func() error {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--nico-test-timeout: Not a number: %s\n", s)
			}
			if num <= 0 {
				return fmt.Errorf("--nico-test-timeout: Invalid: %d: must be greater than or equal to 1\n", num)
			}
			opt.NicoTestTimeout = num
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
		Parser{regexp.MustCompile(`\A(?i)--?(?:z|zip)-?(?:2|to)-?(?:m|mp4)\z`), func() error {
			opt.Command = "ZIP2MP4"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?(?:d|db|sqlite3?)-?(?:2|to)-?(?:m|mp4)\z`), func() error {
			opt.Command = "DB2MP4"
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?login-?only(?:=(on|off))?\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.NicoLoginOnly = true
				dbConfSet(db, "NicoLoginOnly", opt.NicoLoginOnly)
			} else if strings.EqualFold(match[1], "off") {
				opt.NicoLoginOnly = false
				dbConfSet(db, "NicoLoginOnly", opt.NicoLoginOnly)
			} else {
				opt.NicoLoginOnly = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?hls-?only(?:=(on|off))?\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.NicoHlsOnly = true
				dbConfSet(db, "NicoHlsOnly", opt.NicoHlsOnly)
			} else if strings.EqualFold(match[1], "off") {
				opt.NicoHlsOnly = false
				dbConfSet(db, "NicoHlsOnly", opt.NicoHlsOnly)
			} else {
				opt.NicoHlsOnly = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?rtmp-?only(?:=(on|off))?\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.NicoRtmpOnly = true
				dbConfSet(db, "NicoRtmpOnly", opt.NicoRtmpOnly)
			} else if strings.EqualFold(match[1], "off") {
				opt.NicoRtmpOnly = false
				dbConfSet(db, "NicoRtmpOnly", opt.NicoRtmpOnly)
			} else {
				opt.NicoRtmpOnly = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?fast-?ts(?:=(on|off))?\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.NicoFastTs = true
				dbConfSet(db, "NicoFastTs", opt.NicoFastTs)
			} else if strings.EqualFold(match[1], "off") {
				opt.NicoFastTs = false
				dbConfSet(db, "NicoFastTs", opt.NicoFastTs)
			} else {
				opt.NicoFastTs = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?auto-?convert(?:=(on|off))?\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.NicoAutoConvert = true
				dbConfSet(db, "NicoAutoConvert", opt.NicoAutoConvert)
			} else if strings.EqualFold(match[1], "off") {
				opt.NicoAutoConvert = false
				dbConfSet(db, "NicoAutoConvert", opt.NicoAutoConvert)
			} else {
				opt.NicoAutoConvert = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?auto-?delete-?mode\z`), func() error {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--nico-auto-delete-mode: Not a number: %s\n", s)
			}
			if num < 0 || 2 < num {
				return fmt.Errorf("--nico-auto-delete-mode: Invalid: %d: one of 0, 1, 2\n", num)
			}

			opt.NicoAutoDeleteDBMode = num
			dbConfSet(db, "NicoAutoDeleteDBMode", opt.NicoAutoDeleteDBMode)

			return nil
		}},

		Parser{regexp.MustCompile(`\A(?i)--?nico-?(?:u|ultra)fast-?ts\z`), func() error {
			opt.NicoUltraFastTs = true
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
		Parser{regexp.MustCompile(`\A(?i)--?nico-?hls-?port\z`), func() (err error) {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--nico-hls-port: Not a number: %s\n", s)
			}
			if num <= 0 {
				return fmt.Errorf("--nico-hls-port: Invalid: %d: must be greater than or equal to 1\n", num)
			}
			opt.NicoHlsPort = num
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?limit-?bw\z`), func() (err error) {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--nico-limit-bw: Not a number: %s\n", s)
			}
			opt.NicoLimitBw = num
			dbConfSet(db, "NicoLimitBw", opt.NicoLimitBw)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?ts-?start\z`), func() (err error) {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--nico-ts-start: Not a number %s\n", s)
			}
			opt.NicoTsStart = float64(num)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?ts-?start-?min\z`), func() (err error) {
			s, err := nextArg()
			if err != nil {
				return err
			}
			num, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--nico-ts-start-min: Not a number %s\n", s)
			}
			opt.NicoTsStart = float64(num * 60)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?(?:format|fmt)\z`), func() (err error) {
			s, err := nextArg()
			if err != nil {
				return err
			}
			if s == "" {
				return fmt.Errorf("--nico-format: null string not allowed\n", s)
			}
			opt.NicoFormat = s
			dbConfSet(db, "NicoFormat", opt.NicoFormat)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?test-?(?:format|fmt)\z`), func() (err error) {
			s, err := nextArg()
			if err != nil {
				return err
			}
			if s == "" {
				return fmt.Errorf("--nico-test-format: null string not allowed\n", s)
			}
			opt.NicoFormat = s
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?login\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}
			ar := strings.SplitN(str, ",", 2)
			if len(ar) >= 2 && ar[0] != "" {
				loginId := ar[0]
				loginPass := ar[1]
				opt.NicoLoginAlias = fmt.Sprintf("%x", sha3.Sum256([]byte(loginId)))
				SetNicoLogin(opt.NicoLoginAlias, loginId, loginPass)
				dbConfSet(db, "NicoLoginAlias", opt.NicoLoginAlias)

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
		Parser{regexp.MustCompile(`\A(?i)--?nico-?debug\z`), func() error {
			opt.NicoDebug = true
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i).+\.zip\z`), func() (err error) {
			switch opt.Command {
			case "", "ZIP2MP4":
				opt.Command = "ZIP2MP4"
				opt.ZipFile = match[0]
			default:
				return fmt.Errorf("%s: Use -- option before \"%s\"", opt.Command, match[0])
			}
			return
		}},
		Parser{regexp.MustCompile(`\A(?i).+\.sqlite3\z`), func() (err error) {
			switch opt.Command {
			case "", "DB2MP4":
				opt.Command = "DB2MP4"
				opt.DBFile = match[0]
			default:
				return fmt.Errorf("%s: Use -- option before \"%s\"", opt.Command, match[0])
			}
			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?conv-?ext(?:=(mp4|ts))\z`), func() error {
			if strings.EqualFold(match[1], "mp4") {
				opt.ConvExt = "mp4"
			} else if strings.EqualFold(match[1], "ts") {
				opt.ConvExt = "ts"
			}
			dbConfSet(db, "ConvExt", opt.ConvExt)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?extract(?:-?chunks)?(?:=(on|off))\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.ExtractChunks = true
			} else if strings.EqualFold(match[1], "off") {
				opt.ExtractChunks = false
			}
			dbConfSet(db, "ExtractChunks", opt.ExtractChunks)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?force-?(?:re?sv|reservation)(?:=(on|off))\z`), func() error {
			if strings.EqualFold(match[1], "on") {
				opt.NicoForceResv = true
			} else if strings.EqualFold(match[1], "off") {
				opt.NicoForceResv = false
			}
			dbConfSet(db, "NicoForceResv", opt.NicoForceResv)
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?yt-?api-?key\z`), func() (err error) {
			s, err := nextArg()
			if err != nil {
				return
			}
			if s == "" {
				return fmt.Errorf("--yt-api-key: null string not allowed\n", s)
			}
			err = SetYoutubeApiKey(s)
			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?yt-?no-?streamlink(?:=(on|off))?\z`), func() (err error) {
			if strings.EqualFold(match[1], "on") {
				opt.YtNoStreamlink = true
				dbConfSet(db, "YtNoStreamlink", opt.YtNoStreamlink)
			} else if strings.EqualFold(match[1], "off") {
				opt.YtNoStreamlink = false
				dbConfSet(db, "YtNoStreamlink", opt.YtNoStreamlink)
			} else {
				opt.YtNoStreamlink = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?yt-?no-?youtube-?dl(?:=(on|off))?\z`), func() (err error) {
			if strings.EqualFold(match[1], "on") {
				opt.YtNoYoutubeDl = true
				dbConfSet(db, "YtNoYoutubeDl", opt.YtNoYoutubeDl)
			} else if strings.EqualFold(match[1], "off") {
				opt.YtNoYoutubeDl = false
				dbConfSet(db, "YtNoYoutubeDl", opt.YtNoYoutubeDl)
			} else {
				opt.YtNoYoutubeDl = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?nico-?skip-?hb(?:=(on|off))?\z`), func() (err error) {
			if strings.EqualFold(match[1], "on") {
				opt.NicoSkipHb = true
				dbConfSet(db, "NicoSkipHb", opt.NicoSkipHb)
			} else if strings.EqualFold(match[1], "off") {
				opt.NicoSkipHb = false
				dbConfSet(db, "NicoSkipHb", opt.NicoSkipHb)
			} else {
				opt.NicoSkipHb = true
			}
			return nil
		}},
		Parser{regexp.MustCompile(`\A(?i)--?http-?root-?ca\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}
			opt.HttpRootCA = str
			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?http-?skip-?verify(?:=(on|off))?\z`), func() (err error) {
			if strings.EqualFold(match[1], "on") {
				opt.HttpSkipVerify = true
				dbConfSet(db, "HttpSkipVerify", opt.HttpSkipVerify)
			} else if strings.EqualFold(match[1], "off") {
				opt.HttpSkipVerify = false
				dbConfSet(db, "HttpSkipVerify", opt.HttpSkipVerify)
			} else {
				opt.HttpSkipVerify = true
			}

			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?http-?proxy\z`), func() (err error) {
			str, err := nextArg()
			if err != nil {
				return
			}
			if !strings.Contains(str, "://") {
				str = "http://" + str
			}
			opt.HttpProxy = str
			return
		}},
		Parser{regexp.MustCompile(`\A(?i)--?no-?chdir\z`), func() (err error) {
			opt.NoChdir = true
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
			if opt.TcasId != "" {
				fmt.Printf("Unknown option: %s\n", arg)
				Help()
			}
			if ma := regexp.MustCompile(`(?:.*/)?([^/]+)\z`).FindStringSubmatch(arg); len(ma) > 0 {
				opt.TcasId = ma[1]
				return true
			}
		case "ZIP2MP4":
			if ma := regexp.MustCompile(`(?i)\.zip`).FindStringSubmatch(arg); len(ma) > 0 {
				opt.ZipFile = arg
				return true
			}
		case "DB2MP4":
			if ma := regexp.MustCompile(`(?i)\.sqlite3`).FindStringSubmatch(arg); len(ma) > 0 {
				opt.DBFile = arg
				return true
			}
			return false
		} // end switch
		return false
	}

LB_ARG:
	for len(args) > 0 {
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
			if ok := checkFILE(arg); !ok {
				fmt.Printf("Unknown option: %v\n", arg)
				Help()
			}
		}
	}

	if opt.ConfFile == "" {
		opt.ConfFile = fmt.Sprintf("%s.conf", getCmd())
	}

	// [deprecated]
	// load session info
	if data, e := cryptoconf.Load(opt.ConfFile, opt.ConfPass); e != nil {
		err = e
		return
	} else {
		loginId, _ := data["NicoLoginId"].(string)
		if loginId != "" {
			loginPass, _ := data["NicoLoginPass"].(string)
			hash := fmt.Sprintf("%x", sha3.Sum256([]byte(loginId)))
			SetNicoLogin(hash, loginId, loginPass)
			if opt.NicoLoginAlias == "" {
				opt.NicoLoginAlias = hash
				dbConfSet(db, "NicoLoginAlias", opt.NicoLoginAlias)
			}
			os.Remove(opt.ConfFile)
		}
	}

	// prints
	switch opt.Command {
	case "NICOLIVE":
		fmt.Printf("Conf(NicoLoginOnly): %#v\n", opt.NicoLoginOnly)
		fmt.Printf("Conf(NicoFormat): %#v\n", opt.NicoFormat)
		fmt.Printf("Conf(NicoLimitBw): %#v\n", opt.NicoLimitBw)
		fmt.Printf("Conf(NicoHlsOnly): %#v\n", opt.NicoHlsOnly)
		fmt.Printf("Conf(NicoRtmpOnly): %#v\n", opt.NicoRtmpOnly)
		fmt.Printf("Conf(NicoFastTs): %#v\n", opt.NicoFastTs)
		fmt.Printf("Conf(NicoAutoConvert): %#v\n", opt.NicoAutoConvert)
		if opt.NicoAutoConvert {
			fmt.Printf("Conf(NicoAutoDeleteDBMode): %#v\n", opt.NicoAutoDeleteDBMode)
			fmt.Printf("Conf(ExtractChunks): %#v\n", opt.ExtractChunks)
			fmt.Printf("Conf(ConvExt): %#v\n", opt.ConvExt)
		}
		fmt.Printf("Conf(NicoForceResv): %#v\n", opt.NicoForceResv)
		fmt.Printf("Conf(NicoSkipHb): %#v\n", opt.NicoSkipHb)

	case "YOUTUBE":
		fmt.Printf("Conf(YtNoStreamlink): %#v\n", opt.YtNoStreamlink)
		fmt.Printf("Conf(YtNoYoutubeDl): %#v\n", opt.YtNoYoutubeDl)

	case "TWITCAS":
		fmt.Printf("Conf(TcasRetry): %#v\n", opt.TcasRetry)
		fmt.Printf("Conf(TcasRetryTimeoutMinute): %#v\n", opt.TcasRetryTimeoutMinute)
		fmt.Printf("Conf(TcasRetryInterval): %#v\n", opt.TcasRetryInterval)
	case "DB2MP4":
		fmt.Printf("Conf(ExtractChunks): %#v\n", opt.ExtractChunks)
		fmt.Printf("Conf(ConvExt): %#v\n", opt.ConvExt)
	}
	fmt.Printf("Conf(HttpSkipVerify): %#v\n", opt.HttpSkipVerify)

	// check
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
	case "NICOLIVE_TEST":
	case "TWITCAS":
		if opt.TcasId == "" {
			Help()
		}
	case "ZIP2MP4":
		if opt.ZipFile == "" {
			Help()
		}
	case "DB2MP4":
		if opt.DBFile == "" {
			Help()
		}
	default:
		fmt.Printf("[FIXME] options.go/argcheck for %s\n", opt.Command)
		os.Exit(1)
	}

	return
}

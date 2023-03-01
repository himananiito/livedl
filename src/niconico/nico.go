package niconico

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/cookiejar"
	_ "net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/himananiito/livedl/httpbase"
	"github.com/himananiito/livedl/options"
)

func joinCookie(cookies []*http.Cookie) (result string) {
	result = ""
	for _, v := range cookies {
		result += v.String() + "; "
	}
	return result
}

func NicoLogin(opt options.Option) (err error) {

	id, pass, _, _ := options.LoadNicoAccount(opt.NicoLoginAlias)

	if id == "" || pass == "" {
		err = fmt.Errorf("Login ID/Password not set. Use -nico-login \"<id>,<password>\"")
		return
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return
	}

	resp, err, neterr := httpbase.PostForm(
		"https://account.nicovideo.jp/login/redirector?show_button_twitter=1&site=niconico&show_button_facebook=1&next_url=%2F",
		map[string]string{
			"Origin":  "https://account.nicovideo.jp",
			"Referer": "https://account.nicovideo.jp/login",
		},
		jar,
		url.Values{"mail_tel": {id}, "password": {pass}},
	)
	if err != nil {
		return
	}
	if neterr != nil {
		err = neterr
		return
	}
	defer resp.Body.Close()

	// cookieによって判定
	if opt.NicoDebug {
		fmt.Fprintf(os.Stderr, "%v\n", resp.Request.Response.Header)
		fmt.Fprintln(os.Stderr, "StatusCode:", resp.StatusCode)
	}
	//fmt.Println("StatusCode:", resp.Request.Response.StatusCode) // 302
	set_cookie_url, _ := url.Parse("https://www.nicovideo.jp/")
	cookie := joinCookie(jar.Cookies(set_cookie_url))
	if opt.NicoDebug {
		fmt.Fprintln(os.Stderr, "cookie:", cookie)
	}
	var body []byte
	if ma := regexp.MustCompile(`mfa_session=`).FindStringSubmatch(cookie); len(ma) > 0 {
		//2段階認証処理
		fmt.Println("login MFA(2FA)")
		loc := resp.Request.Response.Header.Values("Location")[0]
		//fmt.Fprintln(os.Stderr, "Location:",loc)
		resp, err, neterr = httpbase.Get(
			loc,
			map[string]string{
				"Origin":  "https://account.nicovideo.jp",
				"Referer": "https://account.nicovideo.jp/login",
			},
			jar)
		if err != nil {
			err = fmt.Errorf("login MFA error")
			return
		}
		if neterr != nil {
			err = neterr
			return
		}
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			err = fmt.Errorf("login MFA read error 1")
			return
		}
		//fmt.Printf("%s", body)
		str := string(body)
		if ma = regexp.MustCompile(`/mfa\?site=niconico`).FindStringSubmatch(str); len(ma) <= 0 {
			err = fmt.Errorf("login MFA read error 2")
			return
		}
		//actionから抜き出して loc にセット
		if ma = regexp.MustCompile(`form action=\"([^\"]+)\"`).FindStringSubmatch(str); len(ma) <= 0 {
			err = fmt.Errorf("login MFA read error 3")
			return
		}
		loc = "https://account.nicovideo.jp" + ma[1]
		if opt.NicoDebug {
			fmt.Fprintln(os.Stderr, "Location:", loc)
		}
		//6 digits code を入力
		otp := ""
		retry := 3
		for retry > 0 {
			fmt.Println("Enter 6 digits code (CANCEL: c/q/x):")
			fmt.Scan(&otp) // データを格納する変数のアドレスを指定
			//p = Pattern.compile("^[0-9]{6}$");
			if ma = regexp.MustCompile(`[cqxCQX]{1}`).FindStringSubmatch(otp); len(ma) > 0 {
				err = fmt.Errorf("login MFA : cancel")
				return
			}
			if ma = regexp.MustCompile(`^[0-9]{6}$`).FindStringSubmatch(otp); len(ma) > 0 {
				retry = 99
				break
			}
			retry--
		}
		//fmt.Println("code:",otp)
		if retry <= 0 {
			err = fmt.Errorf("login MFA : wrong digits code")
			return
		}
		resp, err, neterr = httpbase.PostForm(
			loc,
			map[string]string{
				"Origin":  "https://account.nicovideo.jp",
				"Referer": "https://account.nicovideo.jp/login",
			},
			jar,
			url.Values{"otp": {otp}},
		)
		if err != nil {
			err = fmt.Errorf("login MFA POST error")
			return
		}
		if neterr != nil {
			err = neterr
			return
		}
		//結果が302
		cookie = joinCookie(jar.Cookies(set_cookie_url))
		//fmt.Fprintln("StatusCode:", resp.Request.Response.StatusCode) // 302
		if opt.NicoDebug {
			fmt.Fprintln(os.Stderr, "StatusCode:", resp.StatusCode)
			fmt.Fprintln(os.Stderr, "MFA cookie:", cookie)
		}
	}
	//Cookieからuser_sessionの値を読み込む
	if ma := regexp.MustCompile(`user_session=(user_session_.+?);`).FindStringSubmatch(cookie); len(ma) > 0 {
		fmt.Println("session_key: ", string(ma[1]))
		options.SetNicoSession(opt.NicoLoginAlias, string(ma[1]))
		fmt.Println("login success")
	} else {
		err = fmt.Errorf("login failed: session_key not found")
	}
	return
}

func Record(opt options.Option) (hlsPlaylistEnd bool, dbName string, err error) {

	for i := 0; i < 2; i++ {
		// load session info
		if opt.NicoCookies != "" {
			opt.NicoSession, err = NicoBrowserCookies(opt)
			if err != nil {
				return
			}
		} else if opt.NicoSession == "" || i > 0 {
			_, _, opt.NicoSession, _ = options.LoadNicoAccount(opt.NicoLoginAlias)
		}

		var done bool
		var notLogin bool
		var reserved bool
		done, hlsPlaylistEnd, notLogin, reserved, dbName, err = NicoRecHls(opt)
		if done {
			return
		}
		if err != nil {
			return
		}
		if notLogin {
			fmt.Println("not_login")
			if err = NicoLogin(opt); err != nil {
				return
			}
			continue
		}
		if reserved {
			continue
		}

		break
	}

	return
}

func TestRun(opt options.Option) (err error) {

	go func() {
		fmt.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	if false {
		ch := make(chan os.Signal, 10)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-ch
			os.Exit(0)
		}()
	}

	var nextId func() string

	if opt.NicoLiveId == "" {
		// niconama alert

		if opt.NicoTestTimeout <= 0 {
			opt.NicoTestTimeout = 12
		}

		resp, e, nete := httpbase.Get("https://live.nicovideo.jp/api/getalertinfo", nil, nil)
		if e != nil {
			err = e
			return
		}
		if nete != nil {
			err = nete
			return
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case 200:
		default:
			err = fmt.Errorf("StatusCode is %v", resp.StatusCode)
			return
		}

		type Alert struct {
			User     string `xml:"user_id"`
			UserHash string `xml:"user_hash"`
			Addr     string `xml:"ms>addr"`
			Port     string `xml:"ms>port"`
			Thread   string `xml:"ms>thread"`
		}
		status := &Alert{}
		dat, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()

		err = xml.Unmarshal(dat, status)
		if err != nil {
			fmt.Println(string(dat))
			fmt.Printf("error: %v", err)
			return
		}

		raddr, e := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%s", status.Addr, status.Port))
		if e != nil {
			fmt.Printf("%v\n", e)
			return
		}

		conn, e := net.DialTCP("tcp", nil, raddr)
		if e != nil {
			err = e
			return
		}
		defer conn.Close()

		msg := fmt.Sprintf(`<thread thread="%s" version="20061206" res_from="-1"/>%c`, status.Thread, 0)
		if _, err = conn.Write([]byte(msg)); err != nil {
			fmt.Println(err)
			return
		}

		rdr := bufio.NewReader(conn)

		chLatest := make(chan string, 1000)
		go func() {
			for {
				s, e := rdr.ReadString(0)
				if e != nil {
					fmt.Println(e)
					err = e
					return
				}
				//fmt.Println(s)
				if ma := regexp.MustCompile(`>(\d+),\S+,\S+<`).FindStringSubmatch(s); len(ma) > 0 {
				L0:
					for {
						select {
						case <-chLatest:
						default:
							break L0
						}
					}
					chLatest <- ma[1]
				}
			}
		}()

		nextId = func() string {
		L1:
			for {
				select {
				case <-chLatest:
				default:
					break L1
				}
			}
			return <-chLatest
		}

	} else {
		// start from NicoLiveId
		var id int64
		if ma := regexp.MustCompile(`\Alv(\d+)\z`).FindStringSubmatch(opt.NicoLiveId); len(ma) > 0 {
			if id, err = strconv.ParseInt(ma[1], 10, 64); err != nil {
				fmt.Println(err)
				return
			}
		} else {
			fmt.Println("TestRun: NicoLiveId not specified")
			return
		}

		nextId = func() (s string) {
			s = fmt.Sprintf("%d", id)
			id++
			return
		}
	}

	if opt.NicoTestTimeout <= 0 {
		opt.NicoTestTimeout = 3
	}

	//chErr := make(chan error)
	var NFCount int
	var endCount int
	for {
		opt.NicoLiveId = fmt.Sprintf("lv%s", nextId())

		fmt.Fprintf(os.Stderr, "start test: %s\n", opt.NicoLiveId)
		fmt.Fprintf(os.Stderr, "# NumGoroutine: %d\n", runtime.NumGoroutine())

		var msg string
		_, _, err = Record(opt)
		if err != nil {
			if ma := regexp.MustCompile(`\AError\s+code:\s*(\S+)`).FindStringSubmatch(err.Error()); len(ma) > 0 {
				msg = ma[1]
				switch ma[1] {
				case "notfound", "closed", "comingsoon", "timeshift_ticket_exhaust":
				case "deletedbyuser", "deletedbyvisor", "violated":
				case "usertimeshift", "tsarchive", "require_community_member",
					"noauth", "full", "premium_only", "selected-country":
				default:
					fmt.Fprintf(os.Stderr, "unknown: %s\n", ma[1])
					return
				}

			} else if strings.Contains(err.Error(), "closed network") {
				msg = "OK"
			} else {
				fmt.Fprintln(os.Stderr, err)
				return
			}
		} else {
			msg = "OK"
		}

		fmt.Fprintf(os.Stderr, "%s: %s\n---------\n", opt.NicoLiveId, msg)

		endCount++
		if endCount > 100 {
			break
		}

		if msg == "notfound" {
			NFCount++
		} else {
			NFCount = 0
		}
		if NFCount >= 10 {
			return
		}
	}
	return
}

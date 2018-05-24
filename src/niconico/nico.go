
package niconico

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"../cryptoconf"
	"../options"
	"io/ioutil"
	"regexp"
	"strconv"
	"os"
)

func NicoLogin(id, pass string, opt options.Option) (err error) {
	if id == "" || pass == "" {
		err = fmt.Errorf("Login ID/Password not set. Use -nico-login \"<id>,<password>\"")
		return
	}
	tr := &http.Transport {
	//	IdleConnTimeout: 10 * time.Second,
	}
	client := &http.Client{Transport: tr}

	values := url.Values{"mail_tel": {id}, "password": {pass}, "site": {"nicoaccountsdk"}}
	req, _ := http.NewRequest("POST", "https://account.nicovideo.jp/api/v1/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	if ma := regexp.MustCompile(`<session_key>(.+?)</session_key>`).FindSubmatch(body); len(ma) > 0 {
		data := map[string]string{"NicoSession": string(ma[1])}
		if err = cryptoconf.Set(data, opt.ConfFile, opt.ConfPass); err != nil {
			return
		}
		fmt.Println("login success")
	} else {
		err = fmt.Errorf("login failed: session_key not found")
		return
	}
	return
}

func Record(opt options.Option) (err error) {
	setData := map[string]string{}
	if opt.NicoLoginId != "" || opt.NicoLoginPass != "" {
		setData["NicoLoginId"] = opt.NicoLoginId
		setData["NicoLoginPass"] = opt.NicoLoginPass
	}
	if opt.NicoSession != "" {
		setData["NicoSession"] = opt.NicoSession
	}
	if len(setData) > 0 {
		if err = cryptoconf.Set(setData, opt.ConfFile, opt.ConfPass); err != nil {
			return
		}
	}

	for i := 0; i < 2; i++ {
		// load session info
		if data, e := cryptoconf.Load(opt.ConfFile, opt.ConfPass); e != nil {
			err = e
			return
		} else {
			opt.NicoLoginId, _ = data["NicoLoginId"].(string)
			opt.NicoLoginPass, _ = data["NicoLoginPass"].(string)
			opt.NicoSession, _ = data["NicoSession"].(string)
		}

		if (! opt.NicoRtmpOnly) {
			done, notLogin, e := NicoRecHls(opt)
			if done {
				return
			}
			if e != nil {
				err = e
				return
			}
			if notLogin {
				fmt.Println("not_login")
				if err = NicoLogin(opt.NicoLoginId, opt.NicoLoginPass, opt); err != nil {
					return
				}
				continue
			}
		}

		if (! opt.NicoHlsOnly) {
			notLogin, e := NicoRecRtmp(opt)
			if e != nil {
				err = e
				return
			}
			if notLogin {
				fmt.Println("not_login")
				if err = NicoLogin(opt.NicoLoginId, opt.NicoLoginPass, opt); err != nil {
					return
				}
				continue
			}
		}

		break
	}

	return
}

func TestRun(opt options.Option) (err error) {
	var start int64
	if ma := regexp.MustCompile(`\Alv(\d+)\z`).FindStringSubmatch(opt.NicoLiveId); len(ma) > 0 {
		if start, err = strconv.ParseInt(ma[1], 10, 64); err != nil {
			fmt.Println(err)
			return
		}
	} else {
		fmt.Println("TestRun: NicoLiveId not specified")
		return
	}

	opt.NicoRtmpIndex = map[int]bool{
		0: true,
	}

	if opt.NicoTestTimeout <= 0 {
		opt.NicoTestTimeout = 3
	}

	//chErr := make(chan error)
	var NFCount int
	for id := start; id < (start + 100) ; id++ {
		opt.NicoLiveId = fmt.Sprintf("lv%d", id)

		fmt.Fprintf(os.Stderr, "start test: %s\n", opt.NicoLiveId)
		var msg string
		err = Record(opt)
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

		fmt.Fprintf(os.Stderr, "%s: %s\n", opt.NicoLiveId, msg)
		id++

		if msg == "notfound" {
			NFCount++
		} else {
			NFCount = 0
		}
		if NFCount >= 10 {
			return
		}
	}
}
package niconico

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
	"regexp"
	"html"
	"io/ioutil"

	"os"
	"strconv"
	"encoding/json"
	"github.com/gorilla/websocket"
	"../options"
	"../files"
	"../obj"
	"os/signal"
	"sync"
	"strings"
	"syscall"
	"log"
	"runtime"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/sha3"

	_ "net/http/pprof"
	"../httpbase"
	"github.com/gin-gonic/gin"
	"context"
)

type OBJ = map[string]interface{}

type playlist struct {
	uri *url.URL
	uriMaster *url.URL
	uriTimeshift *url.URL
	bandwidth int
	nextTime time.Time
	format string
	withoutFormat bool
	seqNo int
	m3u8ms int64
	position float64
}
type NicoHls struct {
	startDelay int
	playlist playlist

	broadcastId string
	webSocketUrl string
	myUserId string

	wgPlaylist *sync.WaitGroup
	chsPlaylist []chan struct{}

	wgComment *sync.WaitGroup
	chsComment []chan struct{}

	wgMain *sync.WaitGroup
	chsMaster []chan struct{}

	commentStarted bool

	mtxGoCh sync.Mutex
	chInterrupt chan os.Signal
	nInterrupt int

	mtxRestart sync.Mutex
	restartMain bool
	quality string

	errNumChunk int
	errRestartCnt int

	dbName string
	db *sql.DB
	dbMtx sync.Mutex
	lastCommit time.Time

	isTimeshift bool
	timeshiftStart float64

	finish bool
	commentDone bool

	NicoSession string
	limitBw int
}
func NewHls(NicoSession string, opt map[string]interface{}, bw int, format string) (hls *NicoHls, err error) {

	broadcastId, ok := opt["broadcastId"].(string)
	if !ok {
		err = fmt.Errorf("broadcastId is not string")
		return
	}

	webSocketUrl, ok := opt["//webSocketUrl"].(string)
	if !ok {
		err = fmt.Errorf("webSocketUrl is not string")
		return
	}

	myUserId, ok := opt["//myId"].(string)
	if !ok {
		err = fmt.Errorf("userId is not string")
		return
	}
	if myUserId == "" {
		myUserId = "NaN"
	}

	var timeshift bool
	if status, ok := opt["status"].(string); ok && status == "ENDED" {
		timeshift = true
	}



	var pid string
	if nicoliveProgramId, ok := opt["nicoliveProgramId"]; ok {
		pid, _ = nicoliveProgramId.(string)
	}

	var uname string // ユーザ名
	var uid string // ユーザID
	var cname string // コミュ名 or チャンネル名
	var cid string // コミュID or チャンネルID

	var pt string
	if providerType, ok := opt["providerType"]; ok {
		if pt, ok = providerType.(string); ok {
			if pt == "official" {
				uname = "official"
				uid = "official"
				cname = "official"
				cid = "official"
			}
		}
	}

	// ユーザ名
	if userName, ok := opt["userName"]; ok {
		uname, _ = userName.(string)
	}

	// ユーザID
	if userPageUrl, ok := opt["userPageUrl"]; ok {
		if u, ok := userPageUrl.(string); ok {
			if m := regexp.MustCompile(`/user/(\d+)`).FindStringSubmatch(u); len(m) > 0 {
				uid = m[1]
				opt["userId"] = uid
			}
		}
	}
	if uid == "" && pt == "channel" {
		uid = "channel"
	}

	// コミュ名
	if socName, ok := opt["socName"]; ok {
		cname, _ = socName.(string)
	}

	// コミュID
	if comId, ok := opt["comId"]; ok {
		cid, _ = comId.(string)
	}
	if cid == "" {
		if socId, ok := opt["socId"]; ok {
			cid, _ = socId.(string)
		}
	}

	var title string
	if t, ok := opt["title"]; ok {
		title, _ = t.(string)
	}

	// "${PID}-${UNAME}-${TITLE}"
	dbName := format
	dbName = strings.Replace(dbName, "?PID?", files.ReplaceForbidden(pid), -1)
	dbName = strings.Replace(dbName, "?UNAME?", files.ReplaceForbidden(uname), -1)
	dbName = strings.Replace(dbName, "?UID?", files.ReplaceForbidden(uid), -1)
	dbName = strings.Replace(dbName, "?CNAME?", files.ReplaceForbidden(cname), -1)
	dbName = strings.Replace(dbName, "?CID?", files.ReplaceForbidden(cid), -1)
	dbName = strings.Replace(dbName, "?TITLE?", files.ReplaceForbidden(title), -1)
	if timeshift {
		dbName = dbName + "(TS)"
	}
	dbName = dbName + ".sqlite3"

	files.MkdirByFileName(dbName)

	hls = &NicoHls{
		broadcastId: broadcastId,
		webSocketUrl: webSocketUrl,
		myUserId: myUserId,

		wgPlaylist: &sync.WaitGroup{},
		wgComment: &sync.WaitGroup{},
		wgMain: &sync.WaitGroup{},

		quality: "abr",
		dbName: dbName,

		isTimeshift: timeshift,

		NicoSession: NicoSession,
		limitBw: bw,
	}

	for i := 0; i < 2 ; i++ {
		err := hls.dbOpen()
		if err != nil {
			if (! strings.Contains(err.Error(), "able to open")) {
				log.Fatalln(err)
			}
		} else if _, err := os.Stat(hls.dbName); err == nil {
			break
		}

		fmt.Printf("can't open: %s\n", hls.dbName)
		hls.dbName = fmt.Sprintf("%s.sqlite3", pid)
	}

	// 放送情報をdbに入れる。自身のユーザ情報は入れない
	// dbに入れたくないデータはキーの先頭を//としている
	for k, v := range opt {
		if (! strings.HasPrefix(k, "//")) {
			hls.dbKVSet(k, v)
		}
	}

	return
}
func (hls *NicoHls) Close() {
	hls.finalize()
	if hls.db != nil {
		hls.db.Close()
	}
}
func (hls *NicoHls) finalize() {
	//fmt.Println("finalizing")
	hls.dbCommit()
}


// Comment method

func (hls *NicoHls) commentHandler(tag string, attr interface{}) (err error) {
	attrMap, ok := attr.(map[string]interface{})
	if !ok {
		err = fmt.Errorf("[FIXME] commentHandler: not a map: %#v", attr)
		return
	}
	//fmt.Printf("%#v\n", attrMap)
	if vpos_f, ok := attrMap["vpos"].(float64); ok {
		vpos := int(vpos_f)
		var date int
		if d, ok := attrMap["date"].(float64); ok {
			date = int(d)
		}
		var date_usec int
		if d, ok := attrMap["date_usec"].(float64); ok {
			date_usec = int(d)
		}
		date2 := (date * 1000 * 1000) + date_usec
		var user_id string
		if s, ok := attrMap["user_id"].(string); ok {
			user_id = s
		}
		var content string
		if s, ok := attrMap["content"].(string); ok {
			content = s
		}
		calc_s := fmt.Sprintf("%d,%d,%d,%s,%s", vpos, date, date_usec, user_id, content)
		hash := fmt.Sprintf("%x", sha3.Sum256([]byte(calc_s)))

		hls.dbInsert("comment", map[string]interface{}{
			"vpos": attrMap["vpos"],
			"date": attrMap["date"],
			"date_usec": attrMap["date_usec"],
			"date2": date2,
			"no": attrMap["no"],
			"anonymity": attrMap["anonymity"],
			"user_id": attrMap["user_id"],
			"content": attrMap["content"],
			"mail": attrMap["mail"],
			"premium": attrMap["premium"],
			"score": attrMap["score"],
			"thread": attrMap["thread"],
			"origin": attrMap["origin"],
			"locale": attrMap["locale"],
			"hash": hash,
		})
	} else {
		if _, ok := attrMap["thread"].(float64); ok {
			hls.dbKVSet("comment/thread", attrMap["thread"])
		}
	}

return

/*
	var contentExists bool
	var content string

	var keys []string
	for k, v := range attrMap {
		if k == "content" {
			contentExists = true
			content = fmt.Sprintf("%v", v)
			content = strings.Replace(content, "&", "&amp;", -1)
			content = strings.Replace(content, "<", "&lt;", -1)

		} else {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	tagAttrs := []string{tag}
	for _, k := range keys {
		var vStr string
		switch v := attrMap[k].(type) {
		case float64: vStr = fmt.Sprintf("%d", int(v))
		default: vStr = fmt.Sprintf("%v", v)
		}
		vStr = strings.Replace(vStr, `"`, "&quot;", -1)
		tagAttrs = append(tagAttrs, fmt.Sprintf(`%s="%s"`, k, vStr))
	}
	tagAttr := strings.Join(tagAttrs, " ")

	var line string
	if contentExists {
		line = fmt.Sprintf("<%s>%s</%s>\r\n", tagAttr, content, tag)
	} else {
		line = fmt.Sprintf("<%s/>\r\n", tagAttr)
	}

	if err = hls.writeComment(line); err != nil {
		return
	}
*/
	return
}

const (
	OK = iota
	INTERRUPT
	MAIN_WS_ERROR
	MAIN_DISCONNECT
	MAIN_INVALID_STREAM_QUALITY
	PLAYLIST_END
	PLAYLIST_403
	PLAYLIST_ERROR
	DELAY
	COMMENT_WS_ERROR
	COMMENT_SAVE_ERROR
	COMMENT_DONE
	GOT_SIGNAL
	ERROR_SHUTDOWN
	NETWORK_ERROR
)
const (
	START_PLAYLIST = iota
	START_COMMENT
	START_MASTER
)
func (hls *NicoHls) stopPCGoroutines() {
	hls.stopPGoroutines()
	hls.stopCGoroutines()
}
func (hls *NicoHls) stopAllGoroutines() {
	hls.stopPGoroutines()
	hls.stopCGoroutines()
	hls.stopMGoroutines()
}
func (hls *NicoHls) stopPGoroutines() {
	hls.mtxGoCh.Lock()
	defer hls.mtxGoCh.Unlock()

	for _, c := range hls.chsPlaylist {
		c <-struct{}{}
	}
	hls.chsPlaylist = []chan struct{}{}
}
func (hls *NicoHls) stopCGoroutines() {
	hls.mtxGoCh.Lock()
	defer hls.mtxGoCh.Unlock()

	for _, c := range hls.chsComment {
		c <-struct{}{}
	}
	hls.chsComment = []chan struct{}{}
}
func (hls *NicoHls) stopMGoroutines() {
	hls.mtxGoCh.Lock()
	defer hls.mtxGoCh.Unlock()

	for _, c := range hls.chsMaster {
		c <-struct{}{}
	}
	hls.chsMaster = []chan struct{}{}
}
func (hls *NicoHls) working() bool {
	hls.mtxGoCh.Lock()
	defer hls.mtxGoCh.Unlock()

	return len(hls.chsPlaylist) > 0 || len(hls.chsComment) > 0
}
func (hls *NicoHls) stopInterrupt() {
	if hls.chInterrupt != nil {
		signal.Stop(hls.chInterrupt)
	}
}
func (hls *NicoHls) startInterrupt() {
	if hls.chInterrupt == nil {
		hls.chInterrupt = make(chan os.Signal, 10)
		signal.Notify(hls.chInterrupt, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	}

	hls.startMGoroutine(func(sig chan struct{}) int {
		select {
		case <-hls.chInterrupt:
			hls.nInterrupt++
		fmt.Printf("Interrupt count: %d\n", hls.nInterrupt)
			go func() {
				hls.dbCommit()
			}()
			if hls.nInterrupt >= 2 {
				//hls.dbCommit()
				os.Exit(0)
			}
			return INTERRUPT
		case <-sig:
			return GOT_SIGNAL
		}
	})
}

func (hls *NicoHls) getStartDelay() int {
	hls.mtxRestart.Lock()
	defer hls.mtxRestart.Unlock()

	return hls.startDelay
}
func (hls *NicoHls) markRestartMain(delay int) {
	hls.mtxRestart.Lock()
	defer hls.mtxRestart.Unlock()

	if (! hls.restartMain) && (! hls.finish) {
		hls.startDelay = delay
		hls.restartMain = true
	}
}

func (hls *NicoHls) checkReturnCode(code int) {

	// NEVER restart goroutines here except interrupt handler
	switch code {
	case NETWORK_ERROR:
		delay := hls.getStartDelay()
		if delay < 1 {
			hls.markRestartMain(1)
		} else if delay < 3 {
			hls.markRestartMain(3)
		} else if delay < 13 {
			// if 3,4,5..12
			hls.markRestartMain(delay + 1)
		} else {
			hls.markRestartMain(60)
		}
		hls.stopPCGoroutines()

	case DELAY:
		//log.Println("delay")
	case PLAYLIST_403:
		// 番組終了時、websocketでEND_PROGRAMが来るよりも先にこうなるが、
		// END_PROGRAMを受信するにはwebsocketの再接続が必要
		//log.Println("403")
		if hls.nInterrupt == 0 {
			hls.markRestartMain(0)
		}
		hls.stopPGoroutines()

	case PLAYLIST_END:
		fmt.Println("playlist end.")
		hls.finish = true
		if hls.isTimeshift {
			if hls.commentDone {
				hls.stopPCGoroutines()
			} else {
				fmt.Println("waiting comment")
			}
		} else {
			hls.stopPCGoroutines()
		}

	case MAIN_WS_ERROR:
		hls.stopPGoroutines()

	case MAIN_DISCONNECT:
		hls.stopPCGoroutines()

	case MAIN_INVALID_STREAM_QUALITY:
		hls.markRestartMain(0)
		hls.stopPGoroutines()

	case PLAYLIST_ERROR:
		hls.stopPCGoroutines()

	case COMMENT_WS_ERROR:
		//log.Println("comment websocket error")
		hls.stopCGoroutines()

	case COMMENT_SAVE_ERROR:
		//log.Println("comment save error")
		hls.stopCGoroutines()

	case INTERRUPT:
		hls.startInterrupt()
		hls.stopPCGoroutines()

	case ERROR_SHUTDOWN:
		hls.stopPCGoroutines()

	case COMMENT_DONE:
		hls.commentDone = true
		if hls.finish {
			hls.stopPCGoroutines()
		}

	case OK:
	}
}
func (hls *NicoHls) startGoroutine2(start_t int, f func(chan struct{}) int) {

	stopChan := make(chan struct{}, 10)

	if runtime.NumGoroutine() > 100 {
		log.Fatalln("too many goroutines")
	}

	go func(start_t int){
		//fmt.Printf("goroutine started: %d\n", start_t)
		code := f(stopChan)
		//fmt.Printf("ret goroutine: %d -> %d\n", start_t, code)

		switch start_t {
		case START_PLAYLIST:
//	fmt.Printf("wgPlaylist.Done() %#v\n", hls.wgPlaylist)
			hls.wgPlaylist.Done()
		case START_COMMENT:
//	fmt.Printf("wgComment.Done() %#v\n", hls.wgComment)
			hls.wgComment.Done()
		case START_MASTER:
///	fmt.Printf("wgMain.Done() %#v\n", hls.wgMain)
			hls.wgMain.Done()
		}

		hls.checkReturnCode(code)
	}(start_t)

	hls.mtxGoCh.Lock()
	defer hls.mtxGoCh.Unlock()

	switch start_t {
	case START_PLAYLIST:
//fmt.Printf("wgPlaylist.Add(1) %#v\n", hls.wgPlaylist)
		hls.wgPlaylist.Add(1)
		hls.chsPlaylist = append(hls.chsPlaylist, stopChan)
	case START_COMMENT:
//fmt.Printf("wgComment.Add(1) %#v\n", hls.wgComment)
		hls.wgComment.Add(1)
		hls.chsComment = append(hls.chsComment, stopChan)
	case START_MASTER:
//fmt.Printf("wgMain.Add(1) %#v\n", hls.wgMain)
		hls.wgMain.Add(1)
		hls.chsMaster = append(hls.chsMaster, stopChan)
	default:
		log.Fatalf("[FIXME] not implemented start type = %d\n", start_t)
	}
}
// Of playlist
func (hls *NicoHls) startPGoroutine(f func(chan struct{}) int) {
	if hls.nInterrupt == 0 {
		hls.startGoroutine2(START_PLAYLIST, f)
	}
}
// Of comment
func (hls *NicoHls) startCGoroutine(f func(chan struct{}) int) {
	if hls.nInterrupt == 0 {
		hls.startGoroutine2(START_COMMENT, f)
	}
}
func (hls *NicoHls) startMGoroutine(f func(chan struct{}) int) {
	hls.startGoroutine2(START_MASTER, f)
}

func (hls *NicoHls) waitRestartMain() bool {
	pc, _, _, ok := runtime.Caller(1)
	if ok {
		fn := runtime.FuncForPC(pc)
		if (! strings.HasSuffix(fn.Name(), ".Wait")) {
			log.Printf("[FIXME] Don't call waitRestartMain from %s\n", fn.Name())
		}
	}

	hls.waitPGoroutines()

	hls.mtxRestart.Lock()
	defer hls.mtxRestart.Unlock()
	if hls.restartMain {
		hls.restartMain = false
		hls.wgPlaylist = &sync.WaitGroup{}
		hls.startMain()
		return true
	}
	return false
}

func (hls *NicoHls) waitPGoroutines() {
	hls.wgPlaylist.Wait()
}
func (hls *NicoHls) waitCGoroutines() {
	hls.wgComment.Wait()
}
func (hls *NicoHls) waitMGoroutines() {
	hls.wgMain.Wait()
}
func (hls *NicoHls) waitAllGoroutines() {
	hls.waitPGoroutines()
	hls.waitCGoroutines()
	hls.waitMGoroutines()
}

func (hls *NicoHls) getwaybackkey(threadId string) (waybackkey string, neterr, err error) {

	uri := fmt.Sprintf("http://live.nicovideo.jp/api/getwaybackkey?thread=%s", threadId)
	resp, err, neterr := httpbase.Get(uri, map[string]string{"Cookie": "user_session=" + hls.NicoSession})
	if err != nil {
		return
	}
	if neterr != nil {
		return
	}
	defer resp.Body.Close()

	dat, neterr := ioutil.ReadAll(resp.Body)
	if neterr != nil {
		return
	}

	waybackkey = strings.TrimPrefix(string(dat), "waybackkey=")
	if waybackkey == "" {
		err = fmt.Errorf("waybackkey not found")
		return
	}
	return
}
func (hls *NicoHls) getTsCommentFromWhen() (res_from int, when float64) {
	return hls.dbGetFromWhen()
}

func (hls *NicoHls) startComment(messageServerUri, threadId string) {
	if (! hls.commentStarted) {
		hls.commentStarted = true

		hls.startCGoroutine(func(sig chan struct{}) int {
			defer func(){
				hls.commentStarted = false
			}()

			var err error

			// here blocks several seconds
			conn, _, err := websocket.DefaultDialer.Dial(
				messageServerUri,
				map[string][]string{
					"Origin": []string{"http://live2.nicovideo.jp"},
					"Sec-WebSocket-Protocol": []string{"msg.nicovideo.jp#json"},
					"User-Agent": []string{httpbase.GetUserAgent()},
				},
			)
			if err != nil {
				if hls.nInterrupt == 0 {
					log.Println("comment connect:", err)
				}
				return COMMENT_WS_ERROR
			}

			hls.startCGoroutine(func(sig chan struct{}) int {
				<-sig
				if conn != nil {
					conn.Close()
				}
				return OK
			})

			hls.startCGoroutine(func(sig chan struct{}) int {
				for hls.nInterrupt == 0 {
					select {
						case <-time.After(60 * time.Second):
							if conn != nil {
								if err := conn.WriteJSON(""); err != nil {
									if hls.nInterrupt == 0 {
										log.Println("comment send null:", err)
									}
									return COMMENT_WS_ERROR
								}
							} else {
								return OK
							}
						case <-sig:
							return GOT_SIGNAL
					}
				}
				return OK
			})

			var mtxChatTime sync.Mutex
			var _chatCount int64
			incChatCount := func() {
				mtxChatTime.Lock()
				defer mtxChatTime.Unlock()
				_chatCount++
			}
			getChatCount := func() int64 {
				mtxChatTime.Lock()
				defer mtxChatTime.Unlock()
				return _chatCount
			}

			if hls.isTimeshift {

				hls.startCGoroutine(func(sig chan struct{}) int {
					defer func() {
						fmt.Println("Comment done.")
					}()

					var pre int64
					var finishHint int
					for hls.nInterrupt == 0 {
						select {
							case <-time.After(1 * time.Second):
								c := getChatCount()
								if c == 0 || c == pre {

									waybackkey, neterr, err := hls.getwaybackkey(threadId)
									if neterr != nil {
										return NETWORK_ERROR
									}
									if err != nil {
										log.Printf("getwaybackkey: %v\n", err)
										return OK
									}

									_, when := hls.getTsCommentFromWhen()

									//fmt.Printf("getTsCommentFromWhen %f %d\n", when, res_from)

									err = conn.WriteJSON([]OBJ{
										OBJ{"ping": OBJ{"content": "rs:1"}},
										OBJ{"ping": OBJ{"content": "ps:5"}},
										OBJ{"thread": OBJ{
											"fork": 0,
											"nicoru": 0,
											"res_from": -1000,
											"scores": 1,
											"thread": threadId,
											"user_id": hls.myUserId,
											"version": "20061206",
											"waybackkey": waybackkey,
											"when": when + 1,
											"with_global": 1,
										}},
										OBJ{"ping": OBJ{"content": "pf:5"}},
										OBJ{"ping": OBJ{"content": "rf:1"}},
									})
									if err != nil {
										return NETWORK_ERROR
									}

								} else if c < pre + 100 {
									// 通常,1000カウント弱増えるが、少ししか増えない場合
									finishHint++
									if finishHint > 2 {
										return COMMENT_DONE
									}

								} else {
									finishHint = 0
								}
								pre = c

							case <-sig:
								return GOT_SIGNAL
						}
					}
					return COMMENT_DONE
				})

			} else {
				err = conn.WriteJSON([]OBJ{
					OBJ{"ping": OBJ{"content": "rs:0"}},
					OBJ{"ping": OBJ{"content": "ps:0"}},
					OBJ{"thread": OBJ{
						"fork": 0,
						"nicoru": 0,
						"res_from": -1000,
						"scores": 1,
						"thread": threadId,
						"user_id": hls.myUserId,
						"version": "20061206",
						"with_global": 1,
					}},
					OBJ{"ping": OBJ{"content": "pf:0"}},
					OBJ{"ping": OBJ{"content": "rf:0"}},
				})
				if err != nil {
					if hls.nInterrupt == 0 {
						log.Println("comment send first:", err)
					}
					return COMMENT_WS_ERROR
				}
			}

			for hls.nInterrupt == 0 {
				select {
				case <-sig:
					return GOT_SIGNAL
				default:
					var res interface{}
					// Blocks here
					if err = conn.ReadJSON(&res); err != nil {
						return COMMENT_WS_ERROR
					}

					//fmt.Printf("debug %#v\n", res)

					if data, ok := obj.FindVal(res, "chat"); ok {
						if err := hls.commentHandler("chat", data); err != nil {
							return COMMENT_SAVE_ERROR
						}
						incChatCount()

					} else if data, ok := obj.FindVal(res, "thread"); ok {
						if err := hls.commentHandler("thread", data); err != nil {
							return COMMENT_SAVE_ERROR
						}

					} else if _, ok := obj.FindVal(res, "ping"); ok {
						// nop
					} else {
						fmt.Printf("[FIXME] Unknown Message: %#v\n", res)
					}
				}
			}
			return OK
		})
	}
}

func urlJoin(base *url.URL, uri string) (res *url.URL, err error) {
	u, e := url.Parse(uri)
	if e != nil {
		err = e
		return
	}
	res = base.ResolveReference(u)
	return
}

func getString(uri string) (s string, code int, err, neterr error) {
	resp, err, neterr := httpbase.Get(uri, nil)
	if err != nil {
		return
	}
	if neterr != nil {
		return
	}
	defer resp.Body.Close()

	bs, neterr := ioutil.ReadAll(resp.Body)
	if neterr != nil {
		return
	}

	s = string(bs)

	//fmt.Println("<----" + s + "---->")

	code = resp.StatusCode

	return
}

func getBytes(uri string) (code int, buff []byte, start, tresp, end int64, err, neterr error) {
	start = time.Now().UnixNano()

	resp, err, neterr := httpbase.Get(uri, nil)
	if err != nil {
		return
	}
	if neterr != nil {
		return
	}
	defer resp.Body.Close()

	tresp = time.Now().UnixNano()

	buff, neterr = ioutil.ReadAll(resp.Body)
	if neterr != nil {
		return
	}

	end = time.Now().UnixNano()

	code = resp.StatusCode

	return
}

func (hls *NicoHls) saveMedia(seqno int, uri string) (is403, is404 bool, neterr, err error) {
//fmt.Printf("saveMedia %v %v\n", seqno, uri)

	code, buff, start, tresp, end, err, neterr := getBytes(uri)
	if err != nil || neterr != nil {
		return
	}

	switch code {
	case 403:
		is403 = true
		return
	case 404:
		hls.dbInsert("media", map[string]interface{}{
			"seqno": seqno,
			"current": hls.playlist.seqNo,
			"notfound": 1,
		})
		is404 = true
		return
	}

	data := map[string]interface{}{
		"seqno": seqno,
		"current": hls.playlist.seqNo,
		"size": len(buff),
		"bandwidth": hls.playlist.bandwidth,
		"hdrms": ((tresp - start) / (1000 * 1000)),
		"chunkms": ((end - start) / (1000 * 1000)),
		"data": buff,
	}

	if seqno == hls.playlist.seqNo {
		data["m3u8ms"] = hls.playlist.m3u8ms

		if hls.isTimeshift {
			data["position"] = hls.playlist.position
		}
	}

	hls.dbInsert("media", data)

	return
}

func (hls *NicoHls) getPlaylist1(argUri *url.URL) (is403, isEnd bool, neterr, err error) {

	start := time.Now().UnixNano()

	m3u8, code, err, neterr := getString(argUri.String())
	if err != nil || neterr != nil {
		return
	}
	switch code {
	case 200:
	case 403:
		is403 = true
		return
	default:
		err = fmt.Errorf("playlist code: %d: %s", code, hls.playlist.uri.String())
		return
	}

	end := time.Now().UnixNano()

	re := regexp.MustCompile(`#EXT-X-MEDIA-SEQUENCE:(\d+)`)
	ma := re.FindStringSubmatch(m3u8)
	if len(ma) > 0 {

		// Index m3u8

		// #CURRENT-POSITION:0.0
		// #DMC-CURRENT-POSITION:0.0
		var currentPos float64
		if ma := regexp.MustCompile(`#(?:DMC-)?CURRENT-POSITION:(\d+(?:\.\d+))?`).
			FindStringSubmatch(m3u8); len(ma) > 0 {
			if hls.isTimeshift {
				n, err := strconv.ParseFloat(ma[1], 64)
				if err != nil {
					log.Fatalln(err)
				}
				currentPos = n
				hls.playlist.position = currentPos
			} else {
				// timeshiftじゃないのにCURRENT-POSITIONがあれば終了
				isEnd = true
				return
			}

		} else {
			if hls.isTimeshift {
				currentPos = hls.timeshiftStart
			}
		}

		var seqStart int

		seqStart, err = strconv.Atoi(ma[1])
		if err != nil {
			log.Fatal(err)
		}
		hls.playlist.seqNo = seqStart
		hls.playlist.m3u8ms = (end - start) / (1000 * 1000)

		re := regexp.MustCompile(`#EXTINF:(\d+(?:\.\d+)?)[^\n]*\n(\S+)`)
		ma := re.FindAllStringSubmatch(m3u8, -1)

		if len(ma) == 0 {
			hls.playlist.nextTime = time.Now().Add(time.Second)
			return
		}

		type seq_t struct {
			seqno int
			uri string
		}
		var seqlist []seq_t

		var seqMax int
		var duration float64
		for i, a := range ma {
			seqno := i + hls.playlist.seqNo
			if seqno > seqMax {
				seqMax = seqno
			}

			if hls.isTimeshift || i == 0 {
				d, err := strconv.ParseFloat(a[1], 64)
				if err != nil {
					log.Fatalln(err)
				}

				if hls.isTimeshift {
					duration += d
				} else {
					t := time.Duration(float64(time.Second) * (d + 0.5))
					hls.playlist.nextTime = time.Now().Add(t)
				}
			}

			uri, err := urlJoin(argUri, a[2])
			if err != nil {
				log.Fatalln(err)
			}

			seqlist = append(seqlist, seq_t{
				seqno: seqno,
				uri: uri.String(),
			})

			// メディアのURLがシーケンス番号の部分だけが変わる形式かどうか
			if (! hls.isTimeshift) && (! hls.playlist.withoutFormat) {
				f := strings.Replace(
					strings.Replace(uri.String(), "%", "%%", -1),
					fmt.Sprintf("%d.ts?", seqno),
					"%d.ts?",
					1,
				)

				if hls.playlist.format == "" {
					hls.playlist.format = f

				} else if hls.playlist.format != f {
					fmt.Println(m3u8)
					fmt.Println("[FIXME] media format changed")
					hls.playlist.withoutFormat = true
				}
			}
		}

		if hls.isTimeshift {
			hls.timeshiftStart = currentPos + duration - 0.49
		}

		// prints Current SeqNo
		if hls.isTimeshift {
			sec := int(hls.playlist.position)
			var pos string
			if sec >= 3600 {
				pos += fmt.Sprintf("%02d:%02d:%02d", sec / 3600, (sec % 3600) / 60, sec % 60)
			} else {
				pos += fmt.Sprintf("%02d:%02d", sec / 60, sec % 60)
			}
			fmt.Printf("Current SeqNo: %d, Pos: %s\n", hls.playlist.seqNo, pos)

		} else {
			fmt.Printf("Current SeqNo: %d\n", hls.playlist.seqNo)
		}

		if (! hls.isTimeshift) && (! hls.playlist.withoutFormat) {
			// 404になるまで後ろに戻ってチャンクを取得する
			for i := hls.playlist.seqNo - 1; i >= 0; i-- {
				if hls.dbCheckBack(i) {
					hls.dbMarkNoBack(hls.playlist.seqNo - 1)
					break
				} else if hls.dbCheckSequence(i) {
					continue
				}

				u := fmt.Sprintf(hls.playlist.format, i)
				var is404 bool
				is403, is404, neterr, err = hls.saveMedia(i, u)
				if neterr != nil || err != nil {
					return
				}
				if is403 {
					return
				}
				if is404 {
					hls.dbMarkNoBack(hls.playlist.seqNo - 1)
					break
				}
			}
		}

		// m3u8の通りにチャンクを取得する
		for _, seq := range seqlist {
			if hls.dbCheckSequence(seq.seqno) {
				if seq.seqno == hls.playlist.seqNo {
					hls.dbSetM3u8ms()

					if hls.isTimeshift {
						hls.dbSetPosition()
					}
				}
				continue
			}

			var is404 bool
			is403, is404, neterr, err = hls.saveMedia(seq.seqno, seq.uri)
			if neterr != nil || err != nil {
				return
			}
			if is404 {
				fmt.Printf("sequence 404: %d\n", seq.seqno)
			}
			if is403 {
				return
			}
		}

		if strings.Contains(m3u8, "#EXT-X-ENDLIST") {
			isEnd = true
			return
		}

	} else {
		// Master m3u8
		re := regexp.MustCompile(`#EXT-X-STREAM-INF:(?:[^\n]*[^\n\w-])?BANDWIDTH=(\d+)[^\n]*\n(\S+)`)
		ma := re.FindAllStringSubmatch(m3u8, -1)
		if len(ma) > 0 {
			var maxBw int
			var uri *url.URL
			for _, a := range ma {
				bw, err := strconv.Atoi(a[1])
				if err != nil {
					log.Fatal(err)
				}

				set := func() {
					maxBw = bw
					uri, err = urlJoin(argUri, a[2])
					if err != nil {
						log.Println(err)
					}
				}

				if maxBw == 0 {
					set()

				} else if hls.limitBw > 0 {
					// with limit
					// もし現在値が制限を超えていたら、現在値より小さければセット。
					if hls.limitBw < maxBw && bw < maxBw {
						set()

					// 現在値が制限以下で、制限を超えないかつ現在値より大きければセット。
					} else if maxBw <= hls.limitBw && bw <= hls.limitBw && maxBw < bw {
						set()
					}

				} else {
					// without limit
					if maxBw < bw {
						set()
					}
				}
			}
			if uri == nil {
				log.Fatalln("playlist uri not defined")
			}

			fmt.Printf("BANDWIDTH: %d\n", maxBw)
			hls.playlist.bandwidth = maxBw
			if (! hls.isTimeshift) {
				hls.playlist.uriMaster = argUri
				hls.playlist.uri = uri
			}
			return hls.getPlaylist1(uri)

		} else {
			log.Println("playlist error")
		}
	}
	return
}

func (hls *NicoHls) startPlaylist(uri string) {
	hls.startPGoroutine(func(sig chan struct{}) int {
		hls.playlist = playlist{}
		//hls.playlist.uri = uri
		u, e := url.Parse(uri)
		if e != nil {
			return PLAYLIST_ERROR
		}

		if hls.isTimeshift {
			hls.playlist.uriTimeshift = u
		} else {
			hls.playlist.uri = u
		}

		if hls.isTimeshift {
			hls.timeshiftStart = hls.dbGetLastPosition()
		}

		for hls.nInterrupt == 0 {
			var dur time.Duration
			if (hls.playlist.nextTime.IsZero()) {
				dur = 0
			} else {
				now := time.Now()
				dur = hls.playlist.nextTime.Sub(now)
				if dur < time.Second {
					dur = time.Second
				}
			}

			select {
			case <-time.After(dur):
				var uri *url.URL
				if hls.isTimeshift {
					u := hls.playlist.uriTimeshift.String()
					u = regexp.MustCompile(`&start=\d+(?:\.\d*)?`).ReplaceAllString(u, "")
					u += fmt.Sprintf("&start=%f", hls.timeshiftStart)
					uri, _ = url.Parse(u)
				} else {
					uri = hls.playlist.uri
				}

				//fmt.Println(uri)

				is403, isEnd, neterr, err := hls.getPlaylist1(uri)
				if neterr != nil {
					if hls.nInterrupt == 0 {
						log.Println("playlist:", e)
					}
					return NETWORK_ERROR
				}
				if err != nil {
					if hls.nInterrupt == 0 {
						log.Println("playlist:", e)
					}
					return PLAYLIST_ERROR
				}
				if is403 {
					return PLAYLIST_403
				}
				if isEnd {
					return PLAYLIST_END
				}

			case <-sig:
				return GOT_SIGNAL
			}
		}
		return OK
	})
}
func (hls *NicoHls) startMain() {

	// エラー時はMAIN_*を返すこと
	hls.startPGoroutine(func(sig chan struct{}) int {
		//fmt.Println(hls.webSocketUrl)
		//fmt.Printf("startMain: delay is %d\n", hls.startDelay)
		select {
		case <-time.After(time.Duration(hls.startDelay) * time.Second):
		case <-sig:
			return GOT_SIGNAL
		}

		conn, _, err := websocket.DefaultDialer.Dial(
			hls.webSocketUrl,
			map[string][]string{
				"User-Agent": []string{httpbase.GetUserAgent()},
			},
		)
		if err != nil {
			return NETWORK_ERROR
		}
		if false {
			log.Printf("start ws error tsst")
			hls.startPGoroutine(func(sig chan struct{}) int {
				select {
				case <-time.After(10 * time.Second):
					conn.Close()
					return OK
				case <-sig:
					return GOT_SIGNAL
				}
			})
		}
		hls.startPGoroutine(func(sig chan struct{}) int {
			<-sig
			if conn != nil {
				conn.Close()
			}
			return OK
		})

		err = conn.WriteJSON(OBJ{
			"type": "watch",
			"body": OBJ{
				"command": "playerversion",
				"params": []string{
					"leo",
				},
			},
		})
		if err != nil {
			if hls.nInterrupt == 0 {
				log.Println("websocket playerversion write:", err)
			}
			return NETWORK_ERROR
		}

		err = conn.WriteJSON(OBJ{
			"type": "watch",
			"body": OBJ{
				"command": "getpermit",
				"requirement": OBJ{
					"broadcastId": hls.broadcastId,
					"room": OBJ{
						"isCommentable": true,
						"protocol": "webSocket",
					},
					"route": "",
					"stream": OBJ{
						"isLowLatency": false,
						"priorStreamQuality": hls.quality, //"abr", // high
						"protocol": "hls",
						"requireNewStream": true,
					},
				},
			},
		})
		if err != nil {
			if hls.nInterrupt == 0 {
				log.Println("websocket getpermit write:", err)
			}
			return NETWORK_ERROR
		}

		var playlistStarted bool
		var watchingStarted bool
		var watchinginterval int
		for hls.nInterrupt == 0 {
			select {
			case <-sig:
				return GOT_SIGNAL
			default:
			}
			var res interface{}
			err = conn.ReadJSON(&res)
			if err != nil {
				if hls.nInterrupt == 0 && (! hls.finish) {
					log.Println("websocket read:", err)
				}
				return NETWORK_ERROR
			}

			//fmt.Printf("ReadJSON => %v\n", res)
			_type, ok := obj.FindString(res, "type")
			if (! ok) {
				fmt.Printf("type not found\n")
				continue
			}
			switch _type {
			case "watch":
				if cmd, ok := obj.FindString(res, "body", "command"); ok {
					switch cmd {
					case "watchinginterval":
						if arr, ok := obj.FindArray(res, "body", "params"); ok {
							for _, intf := range arr {
								if str, ok := intf.(string); ok {
									num, e := strconv.Atoi(str)
									if e == nil && num > 0 {
										//hls.SetInterval(num)
										watchinginterval = num
										break
									}
								}
							}
						}

						if (! watchingStarted) && watchinginterval > 0 {
							watchingStarted = true
							hls.startPGoroutine(func(sig chan struct{}) int {
								for {
									select {
									case <-time.After(time.Duration(watchinginterval) * time.Second):
										err := conn.WriteJSON(OBJ{
											"type": "watch",
											"body": OBJ{
												"command": "watching",
												"params": []string{
													hls.broadcastId,
													"-1",
													"0",
												},
											},
										})
										if err != nil {
											if hls.nInterrupt == 0 {
												log.Println("websocket watching:", err)
											}
											return NETWORK_ERROR
										}
									case <-sig:
										return GOT_SIGNAL
									}
								}
							})
						}

					case "currentstream":
						if uri, ok := obj.FindString(res, "body", "currentStream", "uri"); ok {
							if (! playlistStarted) && uri != "" {
								playlistStarted = true
								hls.startPlaylist(uri)
							}
						}

					case "disconnect":
						// print params
						if arr, ok := obj.FindArray(res, "body", "params"); ok {
							fmt.Printf("%v\n", arr)
						}
						//chDone <-true
						return MAIN_DISCONNECT

					case "currentroom":
						// comment
						messageServerUri, ok := obj.FindString(res, "body", "room", "messageServerUri")
						if !ok {
							break
						}
						threadId, ok := obj.FindString(res, "body", "room", "threadId")
						if !ok {
							break
						}
						hls.startComment(messageServerUri, threadId)

					case "statistics":
					case "permit":
					case "servertime":
					case "schedule":
						// nop
					default:
						fmt.Printf("%#v\n", res)
						fmt.Printf("unknown command: %s\n", cmd)
					} // end switch "command"
				}

			case "ping":
				err := conn.WriteJSON(OBJ{
					"type": "pong",
					"body": OBJ{},
				})
				if err != nil {
					if hls.nInterrupt == 0 {
						log.Println("websocket watching:", err)
					}
					return NETWORK_ERROR
				}
			case "error":
				code, ok := obj.FindString(res, "body", "code")
				if (! ok) {
					log.Printf("Unknown error: %#v\n", res)
					return ERROR_SHUTDOWN
				}
				switch code {
				case "INVALID_STREAM_QUALITY":
					// webSocket自体を再接続しないと、コメントサーバが取得できない
					switch hls.quality {
					case "abr":
						hls.quality = "high"
						return MAIN_INVALID_STREAM_QUALITY
					default:
						return ERROR_SHUTDOWN
					}
				case "CONTENT_NOT_READY":
					return ERROR_SHUTDOWN

				default:
					log.Printf("Unknown error: %s\n%#v\n", code, res)
					return ERROR_SHUTDOWN
				}

			default:
				log.Printf("Unknown type: %s\n%#v\n", _type, res)
			} // end switch "type"
		} // for ReadJSON
		return OK
	})
}

func (hls *NicoHls) dbOpen() (err error) {
	db, err := sql.Open("sqlite3", hls.dbName)
	if err != nil {
		return
	}

	hls.db = db

	err = hls.dbCreate()
	if err != nil {
		hls.db.Close()
	}
	return
}

func (hls *NicoHls) serve(hlsPort int) {
	hls.startMGoroutine(func(sig chan struct{}) int {
		gin.SetMode(gin.ReleaseMode)
		router := gin.Default()

		router.GET("", func(c *gin.Context) {
			seqno := hls.dbGetLastSeqNo()
			body := fmt.Sprintf(
`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:1
#EXT-X-MEDIA-SEQUENCE:%d

#EXTINF:1.0,
/ts/%d/test.ts

#EXTINF:1.0,
/ts/%d/test.ts

#EXTINF:1.0,
/ts/%d/test.ts

`, seqno - 2, seqno - 2, seqno - 1, seqno)
			c.Data(http.StatusOK, "application/x-mpegURL", []byte(body))
			return
		})

		router.GET("/ts/:idx/test.ts", func(c *gin.Context) {
			i, _ := strconv.Atoi(c.Param("idx"))
			b := hls.dbGetLastMedia(i)
			c.Data(http.StatusOK, "video/MP2T", b)
			return
		})

		srv := &http.Server{
			Addr:           fmt.Sprintf("127.0.0.1:%d", hlsPort),
			Handler:        router,
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		}

		chLocal := make(chan struct{})
		idleConnsClosed := make(chan struct{})
		defer func(){
			close(chLocal)
		}()
		go func() {
			select {
			case <-chLocal:
			case <-sig:
			}
			if err := srv.Shutdown(context.Background()); err != nil {
				log.Printf("srv.Shutdown: %v\n", err)
			}
			close(idleConnsClosed)
		}()

		// クライアントはlocalhostでなく127.0.0.1で接続すること
		// localhostは遅いため
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("srv.ListenAndServe: %v\n", err)
		}

		<-idleConnsClosed
		return OK
	})
}

func (hls *NicoHls) Wait(testTimeout, hlsPort int) {

	hls.startInterrupt()
	defer hls.stopInterrupt()

	if testTimeout > 0 {
		hls.startMGoroutine(func(sig chan struct{}) int {
			select {
			case <-sig:
				return GOT_SIGNAL
			case <-time.After(time.Duration(testTimeout) * time.Second):
				hls.chInterrupt <- syscall.Signal(1000)
				return OK
			}
		})
	}

	if hlsPort > 0 {
		hls.serve(hlsPort)
	}

	hls.startMain()
	for hls.working() {
		//hls.waitPGoroutines()
		if hls.waitRestartMain() {
			continue
		}
		hls.stopPCGoroutines()
		hls.waitCGoroutines()
	}

	hls.finalize()

	hls.stopAllGoroutines()
	hls.waitAllGoroutines()

	return
}

func getProps(opt options.Option) (props interface{}, notLogin bool, err error) {
	formats := []string{
		"http://live2.nicovideo.jp/watch/%s",
		"http://live.nicovideo.jp/watch/%s",
	}

	for i, format := range formats {
		uri := fmt.Sprintf(format, opt.NicoLiveId)
		req, _ := http.NewRequest("GET", uri, nil)
		if opt.NicoSession != "" {
			req.Header.Set("Cookie", "user_session=" + opt.NicoSession)
		}
		req.Header.Set("User-Agent", httpbase.GetUserAgent())

		client := new(http.Client)
		var redirect bool
		var skip bool
		client.CheckRedirect = func(req *http.Request, via []*http.Request) (err error) {
			//fmt.Printf("redirect to %v\n", req.URL.String())
			// リダイレクトが走ったらHLSでは不可とみなす
			// --> cas.nicovideoかもしれない
			if regexp.MustCompile(`\Ahttps?://cas\.nicovideo\.jp/user/.*`).MatchString(req.URL.String()) {
				redirect = false
			} else if i == 0 && regexp.MustCompile(`\Ahttps?://live\.nicovideo\.jp/gate/.*`).MatchString(req.URL.String()) {
				skip = true
			} else {
				redirect = true
			}
			return nil
		}
		resp, e := client.Do(req)
		if e != nil {
			err = e
			return
		}
		if skip {
			resp.Body.Close()
			continue
		}
		defer resp.Body.Close()

		dat, _ := ioutil.ReadAll(resp.Body)

		if redirect {
			return
		}


		if ma := regexp.MustCompile(`data-props="(.+?)"`).FindSubmatch(dat); len(ma) > 0 {
			str := html.UnescapeString(string(ma[1]))
			if err = json.Unmarshal([]byte(str), &props); err != nil {
				return
			}
			return

		} else if ma := regexp.MustCompile(`user\.login_status\s*=\s*['"](.*?)['"]`).FindSubmatch(dat); len(ma) > 0 {
			switch string(ma[1]) {
			case "not_login":
				notLogin = true
			case "login":
				notLogin = false
				return
			}
		}
	}

	return
}

func NicoRecHls(opt options.Option) (done, notLogin bool, err error) {

	//http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 32

	var props interface{}
	props, notLogin, err = getProps(opt)
	if err != nil {
		//fmt.Println(err)
		return
	}
	if notLogin {
		return
	}

	if false {
		obj.PrintAsJson(props)
		os.Exit(9)
	}

	proplist := map[string][]string{
		// "community"
		"comId": []string{"community", "id"}, // "co\d+"
		// "program"
		"beginTime": []string{"program", "beginTime"}, // integer
		"broadcastId": []string{"program", "broadcastId"}, // "\d+"
		"description": []string{"program", "description"}, // 放送説明
		"endTime": []string{"program", "endTime"}, // integer
		"isFollowerOnly": []string{"program", "isFollowerOnly"}, // bool
		"isPrivate": []string{"program", "isPrivate"}, // bool
		"mediaServerType": []string{"program", "mediaServerType"}, // "DMC"
		"nicoliveProgramId": []string{"program", "nicoliveProgramId"}, // "lv\d+"
		"openTime": []string{"program", "openTime"}, // integer
		"providerType": []string{"program", "providerType"}, // "community"
		"status": []string{"program", "status"}, //
		"userName": []string{"program", "supplier", "name"}, // ユーザ名
		"userPageUrl": []string{"program", "supplier", "pageUrl"}, // "http://www.nicovideo.jp/user/\d+"
		"title": []string{"program", "title"}, // title
		// "site"
		"//webSocketUrl": []string{"site", "relive", "webSocketUrl"}, // "ws://..."
		"serverTime": []string{"site", "serverTime"}, // integer
		// "socialGroup"
		"socDescription": []string{"socialGroup", "description"}, // コミュ説明
		"socId": []string{"socialGroup", "id"}, // "co\d+" or "ch\d+"
		"socLevel": []string{"socialGroup", "level"}, // integer
		"socName": []string{"socialGroup", "name"}, // community name
		"socType": []string{"socialGroup", "type"}, // "community"
		// "user"
		"accountType": []string{"user", "accountType"}, // "premium"
		"//myId": []string{"user", "id"}, // "\d+"
		"isLoggedIn": []string{"user", "isLoggedIn"}, // bool
		"//myNickname": []string{"user", "nickname"}, // string
	}

	kv := map[string]interface{}{}
	for k, a := range proplist {
		v, ok := obj.FindVal(props, a...)
		if ok {
			kv[k] = v
		} else {
			//kv[k] = nil
		}
	}

	for _, k := range []string{
		"broadcastId",
		"//webSocketUrl",
		"//myId",
	} {
		if _, ok := kv[k]; !ok {
			fmt.Printf("%v not found\n", k)
			return
		}
	}

	var format string
	if opt.NicoFormat != "" {
		format = opt.NicoFormat
	} else {
		format = "?PID?-?UNAME?-?TITLE?"
	}

	hls, e := NewHls(opt.NicoSession, kv, opt.NicoLimitBw, format)
	if e != nil {
		err = e
		fmt.Println(err)
		return
	}
	defer hls.Close()


/*
	pageUrl, _ := obj.FindString(props, "broadcaster", "pageUrl")

	if regexp.MustCompile(`\Ahttps?://cas\.nicovideo\.jp/.*?/.*`).MatchString(pageUrl) {
		// 実験放送
		userId, ok := obj.FindString(props, "broadcaster", "id")
		if ! ok {
			fmt.Printf("userId not found")
		}

		nickname, ok := obj.FindString(props, "broadcaster", "nickname")
		if ! ok {
			fmt.Printf("nickname not found")
		}

		communityId, ok := obj.FindString(props, "community", "id")
		if ! ok {
			fmt.Printf("communityId not found")
		}

		status, ok := obj.FindString(props, "program", "status")
		if ! ok {
			fmt.Printf("status not found")
		}
		var isArchive bool
		switch status {
			case "ENDED":
				isArchive = true
		}

	}

	log4gui.Info(fmt.Sprintf("isLoggedIn: %v, user_id: %s, nickname: %s", isLoggedIn, user_id, nickname))

	if opt.NicoLoginOnly {
		if isLoggedIn && user_id != "" {
			// login OK
		} else {
			notLogin = true
			return
		}
	}

*/

	hls.Wait(opt.NicoTestTimeout, opt.NicoHlsPort)

	done = true

	return
}
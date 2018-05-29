package niconico

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
	"regexp"
	"html"
	"io/ioutil"
	"io"
	"os"
	"strconv"
	"encoding/json"
	"github.com/grafov/m3u8"
	"github.com/gorilla/websocket"
	"../options"
	"../files"
	"archive/zip"
	"../obj"
	"os/signal"
	"sync"
	"path/filepath"
	"strings"
	"sort"
	"syscall"
	"log"
	"runtime"
)

type OBJ = map[string]interface{}

type Seq struct {
	no uint64
	url string
}
type NicoMedia struct {
	playlistUrl *url.URL
	fileName string
	fileNameOpened string
	file *os.File
	zipWriter *zip.Writer
	seqNo uint64
	nextTime time.Time
	format string
	ht2_nicolive string
	containList map[uint64]struct{}
	mtx sync.Mutex
}
type comment struct {
	messageServerUri string
	threadId string
}
type NicoHls struct {
	broadcastId string
	webSocketUrl string
	userId string

	media NicoMedia
	msgFile *os.File
	msgMtx sync.Mutex

	wgPlaylist *sync.WaitGroup
	chsPlaylist []chan struct{}

	wgComment *sync.WaitGroup
	chsComment []chan struct{}

	wgMaster *sync.WaitGroup
	chsMaster []chan struct{}

	commentStarted bool

	mtxGoCh sync.Mutex
	chInterrupt chan os.Signal
	nInterrupt int

	mtxRestart sync.Mutex
	restartMain bool
}
func NewHls(broadcastId, webSocketUrl, userId string) (hls *NicoHls, err error) {

	hls = &NicoHls{
		broadcastId: broadcastId,
		webSocketUrl: webSocketUrl,
		userId: userId,

		wgPlaylist: &sync.WaitGroup{},
		wgComment: &sync.WaitGroup{},
		wgMaster: &sync.WaitGroup{},
	}

	return
}
func (hls *NicoHls) Close() {
	//hls.finalize()
}
func (hls *NicoHls) finalize() {
	fmt.Println("finalizing")
	hls.closeZip()
	hls.closeCommentFile()
}

func (hls *NicoHls) closeZip() {
	if hls.media.zipWriter != nil {
		hls.media.zipWriter.Close()
	}
	if hls.media.file != nil {
		hls.media.file.Close()
	}
}
func getPlaylist(uri *url.URL) (nextUri *url.URL, mediapl *m3u8.MediaPlaylist, is403, is404 bool, err error) {
	resp, err := http.Get(uri.String())
	if err != nil {
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
	case 403:
		is403 = true
		return
	case 404:
		is404 = true
		return
	default:
		err = fmt.Errorf("getPlaylist: StatusCode is %v", resp.StatusCode)
		return
	}
	//body, err := ioutil.ReadAll(resp.Body)

	p, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		fmt.Printf("getPlaylist: error resp: %v\n", resp)
		return
		//panic(err)
	}
	switch listType {
	case m3u8.MEDIA:
		nextUri = uri
		mediapl = p.(*m3u8.MediaPlaylist)
		return
	case m3u8.MASTER:
		masterpl := p.(*m3u8.MasterPlaylist)

		var bw uint32
		for _, pl := range masterpl.Variants {
			// select quality
			fmt.Printf("bandwidth: %d\n", pl.Bandwidth)
			if pl.Bandwidth > bw {
				bw = pl.Bandwidth
				u, err := url.Parse(pl.URI)
				if err != nil {
				}
				nextUri = uri.ResolveReference(u)
			}
		}
		fmt.Printf("selected bandwidth: %d\n", bw)
		if nextUri.String() != "" && uri.String() != nextUri.String() {
			nextUri, mediapl, is403, is404, err = getPlaylist(nextUri)
			return
		}
	}
	err = fmt.Errorf("getPlaylist: error: %s", uri.String())
	return
}

func (media *NicoMedia) clearHt2Nicolive() {
	media.ht2_nicolive = ""
}
func (media *NicoMedia) SetPlaylist(uri string) (is403 bool, err error) {
	media.playlistUrl, err = url.Parse(uri)
	if err != nil {
		return
	}
	is403, err = media.GetMedias()
	return
}

func (media *NicoMedia) Contains(num uint64) (ok bool) {
	if media.containList == nil {
		media.containList = make(map[uint64]struct{})
	}
	_, ok = media.containList[num]
	return
}
func (media *NicoMedia) AddContains(num uint64) {
	if media.containList == nil {
		media.containList = make(map[uint64]struct{})
	}
	//fmt.Printf("AddContains: %d\n", num)
	media.containList[num] = struct{}{}
	return
}

func (media *NicoMedia) OpenFile() (err error) {
	if media.fileName == "" {
		err = fmt.Errorf("Filename not set")
		return
	}
	name, err := files.GetFileNameNext(media.fileName)

	f, err := os.Create(name)
	if err != nil {
		return
	}
	media.fileNameOpened = name
	media.file = f

	media.zipWriter = zip.NewWriter(f)

	return
}

func (media *NicoMedia) WriteChunk(seqNo uint64, rdr io.Reader) (err error) {

	buff, err := ioutil.ReadAll(rdr)
	if err != nil {
		return
	}

	media.mtx.Lock()
	defer media.mtx.Unlock()

	if media.zipWriter == nil {
		if err = media.OpenFile(); err != nil {
			return
		}
	}

	name := fmt.Sprintf("%d.ts", seqNo)

	if wr, e := media.zipWriter.Create(name); e != nil {
		err = e
		return
	} else {
		if _, err = wr.Write(buff); err != nil {
			return
		}
	}

	fmt.Printf("SeqNo: %d : %s\n", seqNo, media.fileNameOpened)

	media.AddContains(seqNo)
	return
}
func (media *NicoMedia) GetMedia1(seq Seq) (is403, is404 bool, err error) {
	if media.Contains(seq.no) {
		return
	}

	resp, err := http.Get(seq.url)
	if err != nil {
		return
	}

	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
	case 403:
		is403 = true
		return
	case 404:
		is404 = true
		return
	default:
		err = fmt.Errorf("StatusCode is %v", resp.StatusCode)
		return
	}

	err = media.WriteChunk(seq.no, resp.Body)

	return
}
func (media *NicoMedia) GetMedias() (is403 bool, err error) {
	var mediapl *m3u8.MediaPlaylist
	// fetch .m3u8
	for i := 0; i < 10; i++ {
		var uri *url.URL
		var is404 bool
		uri, mediapl, is403, is404, err = getPlaylist(media.playlistUrl)
		if err != nil {
			fmt.Printf("GetMedias/getPlaylist: %v\n", err)
			return
		}
		if is404 {
			time.Sleep(2 * time.Second)
			continue
		}
		if is403 {
			return
		}
		if uri != nil {
			media.playlistUrl = uri
		}
		break
	}
	if mediapl == nil {
		return
	}

	// 次にplaylistを取得する時刻を設定
	func() {
		for _, seg := range mediapl.Segments {
			if seg == nil {
				break
			}
			d, _ := time.ParseDuration(fmt.Sprintf("%fs", seg.Duration))
			media.nextTime = time.Now().Add(d)
			return
		}
		media.nextTime = time.Now().Add(time.Second)
	}()


	for _, seg := range mediapl.Segments {
		if seg == nil {
			break
		}

		// url
		u, e := url.Parse(seg.URI)
		if e != nil {
			err = e
			return
		}

		mediaUri := media.playlistUrl.ResolveReference(u)
		mUri := mediaUri.String()

		// media url is
		// \d\.ts?ht2_nicolive=\d+\.\w+
		ma := regexp.MustCompile(`/\d+\.ts\?ht2_nicolive=([\w\.]+)\z`).
			FindStringSubmatch(mUri)
		if len(ma) > 0 {
			if media.ht2_nicolive != "" {
				if media.ht2_nicolive != ma[1] {
					fmt.Println("[FIXME] ht2_nicolive changed")
				}
			} else {
				media.ht2_nicolive = ma[1]
			}
		} else {
			fmt.Printf("[FIXME] ht2_nicolive Not found: %s\n", mUri)
		}
	}

	getNicoTsChunk := func(num uint64) (is403, is404 bool, err error) {
		s := fmt.Sprintf("%d.ts?ht2_nicolive=%s", num, media.ht2_nicolive)
		u, e := url.Parse(s)
		if e != nil {
			err = e
			return
		}

		mUri := media.playlistUrl.ResolveReference(u).String()
		//fmt.Println(mUri)

		return media.GetMedia1(Seq{
			no: num,
			url: mUri,
		})
	}

	for i := int64(mediapl.SeqNo) - 1; (i >= 0) && (i > int64(mediapl.SeqNo) - 10); i-- {
		var is404 bool
		is403, is404, err = getNicoTsChunk(uint64(i))
		if err != nil {
			log.Println(err)
			return
			//panic(err)
		}
		if is403 {
			return
		}
		if is404 {
			log.Printf("404: SeqNo=%d\n", i)
			break
		}
	}

	for i := mediapl.SeqNo; true; i++ {
		var is404 bool
		is403, is404, err = getNicoTsChunk(i)
		if err != nil {
			log.Println(err)
			return
			//panic(err)
		}
		if is403 {
			return
		}
		if is404 {
			break
		}
	}

	return
}

func (hls *NicoHls) SetFileName(name string) {
	hls.media.fileName = name
}
// Comment method
func (hls *NicoHls) openCommentFile() (err error) {
	// never Lock() here

	ext := filepath.Ext(hls.media.fileName)
	name := strings.TrimSuffix(hls.media.fileName, ext)
	name = fmt.Sprintf("%s.xml", name)

	name, e := files.GetFileNameNext(name)
	if e != nil {
		err = e
		return
	}

	file, err := os.Create(name)
	if err != nil {
		return
	}
	hls.msgFile = file

	if _, err = hls.msgFile.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n<packet>\r\n"); err != nil {
		return
	}
	return
}
func (hls *NicoHls) writeComment(line string) (err error) {
	hls.msgMtx.Lock()
	defer hls.msgMtx.Unlock()

	if hls.msgFile == nil {
		if err = hls.openCommentFile(); err != nil {
			return
		}
	}

	if _, err = hls.msgFile.WriteString(line); err != nil {
		return
	}
	return
}
func (hls *NicoHls) closeCommentFile() {
	hls.msgMtx.Lock()
	defer hls.msgMtx.Unlock()

	if hls.msgFile != nil {
		hls.msgFile.WriteString("</packet>\r\n")
		hls.msgFile.Close()
		hls.msgFile = nil
	}
}
// namarokuRecorderのxml形式でコメントを保存する
func (hls *NicoHls) commentHandler(tag string, attr interface{}) (err error) {

	attrMap, ok := attr.(map[string]interface{})
	if !ok {
		err = fmt.Errorf("[FIXME] commentHandler: not a map: %#v", attr)
		return
	}

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

	return
}

const (
	OK = iota
	INTERRUPT
	MAIN_WS_ERROR
	MAIN_DISCONNECT
	PLAYLIST_403
	PLAYLIST_ERROR
	DELAY
	COMMENT_WS_ERROR
	COMMENT_SAVE_ERROR
	GOT_SIGNAL
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
			if hls.nInterrupt >= 2 {
				hls.finalize()
				os.Exit(0)
			}
			return INTERRUPT
		case <-sig:
			return GOT_SIGNAL
		}
	})
}
func (hls *NicoHls) markRestartMain() {
	hls.mtxRestart.Lock()
	defer hls.mtxRestart.Unlock()
	hls.restartMain = true
}
func (hls *NicoHls) checkReturnCode(code int) {

	// NEVER restart goroutines here except interrupt handler
	switch code {
	case DELAY:
		//log.Println("delay")
	case PLAYLIST_403:
		// 番組終了時、websocketでEND_PROGRAMが来るよりも先にこうなるが、
		// END_PROGRAMを受信するにはwebsocketの再接続が必要
		//log.Println("403")
		hls.markRestartMain()
		hls.stopPGoroutines()

	case PLAYLIST_ERROR:
		hls.stopPCGoroutines()

	case COMMENT_WS_ERROR:
		//log.Println("comment websocket error")
		hls.stopCGoroutines()

	case COMMENT_SAVE_ERROR:
		//log.Println("comment save error")
		hls.stopCGoroutines()

	case MAIN_DISCONNECT:
		hls.stopPCGoroutines()

	case INTERRUPT:
		hls.startInterrupt()
		hls.stopPCGoroutines()

	case OK:
	}
}
func (hls *NicoHls) startGoroutine2(start_t int, f func(chan struct{}) int) {

	stopChan := make(chan struct{}, 10)

	if runtime.NumGoroutine() > 50 {
		panic("too many goroutines")
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
///	fmt.Printf("wgMaster.Done() %#v\n", hls.wgMaster)
			hls.wgMaster.Done()
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
//fmt.Printf("wgMaster.Add(1) %#v\n", hls.wgMaster)
		hls.wgMaster.Add(1)
		hls.chsMaster = append(hls.chsMaster, stopChan)
	default:
		log.Fatalf("[FIXME] not implemented start type = %d\n", start_t)
	}
}
// Of playlist
func (hls *NicoHls) startPGoroutine(f func(chan struct{}) int) {
	hls.startGoroutine2(START_PLAYLIST, f)
}
// Of comment
func (hls *NicoHls) startCGoroutine(f func(chan struct{}) int) {
	hls.startGoroutine2(START_COMMENT, f)
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
//fmt.Printf("wgPlaylist.Wait() %v\n", hls.wgPlaylist)
	//hls.wgPlaylist = &sync.WaitGroup{}
}
func (hls *NicoHls) waitCGoroutines() {
	hls.wgComment.Wait()
//fmt.Printf("wgComment.Wait() %#v\n", hls.wgComment)

//	hls.wgComment = &sync.WaitGroup{}
}
func (hls *NicoHls) waitMGoroutines() {
	hls.wgMaster.Wait()
//	hls.wgMaster = &sync.WaitGroup{}
}
func (hls *NicoHls) waitAllGoroutines() {
	hls.waitPGoroutines()
	hls.waitCGoroutines()
	hls.waitMGoroutines()
}

func (hls *NicoHls) startComment(messageServerUri, threadId string) {
	if (! hls.commentStarted) {
		hls.commentStarted = true

		hls.startCGoroutine(func(sig chan struct{}) int {
			var err error

			// here blocks several seconds
			conn, _, err := websocket.DefaultDialer.Dial(
				messageServerUri,
				map[string][]string{
					"Origin": []string{"http://live2.nicovideo.jp"},
					"Sec-WebSocket-Protocol": []string{"msg.nicovideo.jp#json"},
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
				for {
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
			})

			err = conn.WriteJSON([]OBJ{
				OBJ{"ping": OBJ{"content": "rs:0"}},
				OBJ{"ping": OBJ{"content": "ps:0"}},
				OBJ{"thread": OBJ{
					"fork": 0,
					"nicoru": 0,
					"res_from": -1000,
					"scores": 1,
					"thread": threadId,
					"user_id": hls.userId,
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

			for {
				select {
				case <-sig:
					return GOT_SIGNAL
				default:
					var res interface{}
					// Blocks here
					if err = conn.ReadJSON(&res); err != nil {
						return COMMENT_WS_ERROR
					}
					if data, ok := obj.FindVal(res, "chat"); ok {
						if err := hls.commentHandler("chat", data); err != nil {
							return COMMENT_SAVE_ERROR
						}

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
		})
	}
}
func (hls *NicoHls) startPlaylist(uri string) {
	hls.startPGoroutine(func(sig chan struct{}) int {
		hls.media.clearHt2Nicolive()
		is403, e := hls.media.SetPlaylist(uri)
		if is403 {
			return PLAYLIST_403
		}
		if e != nil {
			//log.Println(e)
			return PLAYLIST_ERROR
		}

		for {
			var dur time.Duration
			if (hls.media.nextTime.IsZero()) {
				dur = 10 * time.Second
			} else {
				now := time.Now()
				dur = hls.media.nextTime.Sub(now)
			}
			if dur < time.Second {
				dur = time.Second
			}

			select {
			case <-time.After(dur):
				is403, e := hls.media.GetMedias()
				if is403 {
					//hls.startPGoroutine(func(sig chan struct{}) int {
					//	select {
					//	case <-sig:
					//		return GOT_SIGNAL
					//	case <-time.After(1 * time.Second):
					//		return PLAYLIST_403
					//	}
					//})
					//return DELAY
					return PLAYLIST_403

				}
				if e != nil {
					if hls.nInterrupt == 0 {
						log.Println("playlist:", e)
					}
					return PLAYLIST_ERROR
				}
			case <-sig:
				return GOT_SIGNAL
			}
		}
	})
}
func (hls *NicoHls) startMain() {

	hls.startPGoroutine(func(sig chan struct{}) int {

		conn, _, err := websocket.DefaultDialer.Dial(
			hls.webSocketUrl,
			map[string][]string{},
		)
		if err != nil {
			return MAIN_WS_ERROR
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
				log.Println("websocket connect:", err)
			}
			return MAIN_WS_ERROR
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
						"priorStreamQuality": "abr",
						"protocol": "hls",
						"requireNewStream": true,
					},
				},
			},
		})
		if err != nil {
			if hls.nInterrupt == 0 {
				log.Println("websocket first write:", err)
			}
			return MAIN_WS_ERROR
		}

		var playlistStarted bool
		var watchingStarted bool
		var watchinginterval int
		for {
			select {
			case <-sig:
				return GOT_SIGNAL
			default:
			}
			var res interface{}
			err = conn.ReadJSON(&res)
			if err != nil {
				if hls.nInterrupt == 0 {
					log.Println("websocket read:", err)
				}
				return MAIN_WS_ERROR
			}

			///fmt.Printf("ReadJSON => %v\n", res)
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
											return MAIN_WS_ERROR
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
					return MAIN_WS_ERROR
				}
			} // end switch "type"
		} // for ReadJSON
	})
}

func (hls *NicoHls) Wait(testTimeout int) {

	//fmt.Printf("# NumGoroutine: %d\n", runtime.NumGoroutine())

	if testTimeout > 0 {
		hls.startPGoroutine(func(sig chan struct{}) int {
			select {
			case <-sig:
				return GOT_SIGNAL
			case <-time.After(time.Duration(testTimeout) * time.Second):
				return INTERRUPT
			}
		})
	}

	hls.startInterrupt()
	defer hls.stopInterrupt()

	hls.startMain()
	for hls.working() {
		//hls.waitPGoroutines()
		if hls.waitRestartMain() {
			continue
		}
		hls.waitCGoroutines()
	}

	hls.finalize()

	hls.stopAllGoroutines()
	hls.waitAllGoroutines()

	//fmt.Printf("# NumGoroutine: %d\n", runtime.NumGoroutine())

	return
}

func getProps(opt options.Option) (props interface{}, notLogin bool, err error) {
	formats := []string{
		"http://live2.nicovideo.jp/watch/%s",
	//	"http://live.nicovideo.jp/watch/%s",
	}

	for _, format := range formats {
		url := fmt.Sprintf(format, opt.NicoLiveId)
		req, _ := http.NewRequest("GET", url, nil)
		if opt.NicoSession != "" {
			req.Header.Set("Cookie", "user_session=" + opt.NicoSession)
		}

		client := new(http.Client)
		var redirect bool
		client.CheckRedirect = func(req *http.Request, via []*http.Request) (err error) {
			//fmt.Printf("redirect to %v\n", req.URL.String())
			// リダイレクトが走ったらHLSでは不可とみなす
			redirect = true
			return nil
		}
		resp, e := client.Do(req)
		if e != nil {
			err = e
			return
		}
		defer resp.Body.Close()

		if redirect {
			return
		}

		dat, _ := ioutil.ReadAll(resp.Body)

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

	props, __notLogin, err := getProps(opt)
	if err != nil {
		//fmt.Println(err)
		return
	}
	if __notLogin {
		notLogin = true
		return
	}

	if false {
		b, err := json.MarshalIndent(props, "", "  ")
		if err != nil {
			fmt.Println("error:", err)
		}
		fmt.Println(string(b))
	}

	broadcastId, ok := obj.FindString(props, "program", "broadcastId")
	if ! ok {
		fmt.Printf("broadcastId not found\n")
		return
	}

	nicoliveProgramId, ok := obj.FindString(props, "program", "nicoliveProgramId")
	if ! ok {
		fmt.Printf("nicoliveProgramId not found")
		return
	}

	title, ok := obj.FindString(props, "program", "title")
	if ! ok {
		fmt.Printf("title not found")
		return
	}

	title = files.ReplaceForbidden(title)

	communityId, ok := obj.FindString(props, "community", "id")
	if ! ok {
		provider, ok := obj.FindString(props, "program", "providerType")
		if ! ok {
			fmt.Printf("providerType not found")
			return
		}
		communityId = provider // "official"
	}

	webSocketUrl, ok := obj.FindString(props, "site", "relive", "webSocketUrl")
	if ! ok {
		fmt.Printf("webSocketUrl not found")
		return
	}

	user_id, ok := obj.FindString(props, "user", "id")
	if ! ok {
		fmt.Printf("user_id not found\n")
		return
	}

	hls, err := NewHls(broadcastId, webSocketUrl, user_id)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer hls.Close()

	if communityId != "" {
		hls.SetFileName(fmt.Sprintf("%s-%s-%s.zip", nicoliveProgramId, communityId, title))
	} else {
		hls.SetFileName(fmt.Sprintf("%s-%s.zip", nicoliveProgramId, title))
	}

	hls.Wait(opt.NicoTestTimeout)
	done = true

	return
}
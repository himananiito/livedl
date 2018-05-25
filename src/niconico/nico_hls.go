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
	containList map[uint64]bool
	mtx sync.Mutex
}

type NicoHls struct {
	broadcastId string
	webSocketUrl string
	wsConn *websocket.Conn
	watchInterval time.Duration
	nextIntervalTime time.Time
	media NicoMedia
}
func (hls *NicoHls) CloseFile() {
	if hls.media.zipWriter != nil {
		fmt.Println("hls.media.zipWriter.Close")
		hls.media.zipWriter.Close()
	}
	if hls.media.file != nil {
		hls.media.file.Close()
	}
}
func (hls *NicoHls) Close() {
	if hls.wsConn != nil {
		hls.wsConn.Close()
	}
	hls.CloseFile()
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
		err = fmt.Errorf("StatusCode is %v", resp.StatusCode)
		return
	}
	//body, err := ioutil.ReadAll(resp.Body)

	p, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		fmt.Printf("error resp: %v\n", resp)
		panic(err)
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

func (media *NicoMedia) ClearHt2Nicolive() {
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
		media.containList = make(map[uint64]bool)
	}
	_, ok = media.containList[num]
	return
}
func (media *NicoMedia) AddContains(num uint64) {
	if media.containList == nil {
		media.containList = make(map[uint64]bool)
	}
	//fmt.Printf("AddContains: %d\n", num)
	media.containList[num] = true
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

	if true {
		chSig := make(chan os.Signal, 10)
		signal.Notify(chSig, os.Interrupt)
		go func() {
			<-chSig
			media.mtx.Lock()
			if media.zipWriter != nil {
				media.zipWriter.Close()
			}
			os.Exit(0)
		}()
	}

	return
}

func (media *NicoMedia) WriteChunk(seqNo uint64, rdr io.Reader) (err error) {

	if media.zipWriter == nil {
		if err = media.OpenFile(); err != nil {
			return
		}
	}

	buff, err := ioutil.ReadAll(rdr)
	if err != nil {
		return
	}

	name := fmt.Sprintf("%d.ts", seqNo)
	media.mtx.Lock()
	defer media.mtx.Unlock()
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
	for i :=0; i < 10; i++ {
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
			panic(err)
		}
		if is403 {
			return
		}
		if is404 {
			fmt.Printf("404: %d\n", i)
			break
		}
	}

	for i := mediapl.SeqNo; true; i++ {
		var is404 bool
		is403, is404, err = getNicoTsChunk(i)
		if err != nil {
			panic(err)
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

func (hls *NicoHls) SetNextInterval() {
	hls.nextIntervalTime = time.Now().Add(hls.watchInterval * time.Second)
}

func (hls *NicoHls) SetInterval(interval int) {
	hls.watchInterval = time.Duration(interval)
	hls.SetNextInterval()
}

func (hls *NicoHls) SendWatching() (err error) {
	hls.SetNextInterval()

	//fmt.Printf("\n\n\nSending watching\n\n\n")
	err = hls.wsConn.WriteJSON(OBJ{
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
	return
}

func (hls *NicoHls) SendPong() (err error) {
	err = hls.wsConn.WriteJSON(OBJ{
		"type": "pong",
		"body": OBJ{},
	})
	return
}

func (hls *NicoHls) Connect() (err error) {
	hls.wsConn, _, err = websocket.DefaultDialer.Dial(
		hls.webSocketUrl,
		map[string][]string{},
	)
	if err != nil {
		return
	}

	err = hls.wsConn.WriteJSON(OBJ{
		"type": "watch",
		"body": OBJ{
			"command": "playerversion",
			"params": []string{
				"leo",
			},
		},
	})
	if err != nil {
		return
	}

	err = hls.wsConn.WriteJSON(OBJ{
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
		return
	}
	return
}
func NewHls(broadcastId, webSocketUrl string) (hls *NicoHls, err error) {

	hls = &NicoHls{
		broadcastId: broadcastId,
		webSocketUrl: webSocketUrl,
	}

	err = hls.Connect()

	return
}

func (hls *NicoHls) Wait(testTimeout int) (shouldReconnect, done bool, err error) {
	chUrl := make(chan string)
	ch403 := make(chan bool, 10)
	chDone := make(chan bool, 10)

	chEndMain := make(chan bool)
	chEndGo0 := make(chan bool)
	chEndGo1 := make(chan bool)
	chTimeout := make(chan bool)

	if testTimeout > 0 {
		go func() {
			time.Sleep(time.Second * time.Duration(testTimeout))
			chTimeout <- true
		}()
	}

	// playlist loop
	go func() {
		//fmt.Println("Entering goroutine#0")
		defer func() {
			hls.wsConn.Close()
			close(chEndGo0)
			//fmt.Println("goroutine#0 End")
		}()
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
			case uri := <- chUrl:
				hls.media.ClearHt2Nicolive()
				is403, e := hls.media.SetPlaylist(uri)
				if is403 {
					//fmt.Println("goroutine#0 403 detect")
					ch403 <- true
					return
				}
				if e != nil {
					fmt.Println(e)
					return
				}

			case <- time.After(dur):
				//fmt.Println("#0 timeout!")
				is403, e := hls.media.GetMedias()
				if is403 {
					//fmt.Println("goroutine#0 403 detect")
					ch403 <- true
					return
				}
				if e != nil {
					fmt.Println(e)
					return
				}

			case <- chEndMain:
				return
			case <- chTimeout:
				return
			}
		}
	}()

	// ws send watching loop
	go func() {
		//fmt.Println("Entering goroutine#1")
		defer func() {
			hls.wsConn.Close()
			close(chEndGo1)
			//fmt.Println("goroutine#1 End")
		}()
		for {
			var dur time.Duration
			if (hls.nextIntervalTime.IsZero()) {
				dur = 1 * time.Second
			} else {
				now := time.Now()
				dur = hls.nextIntervalTime.Sub(now)
			}

			select {
			case <- time.After(dur):
				//fmt.Println("#1 timeout!")
				if (! hls.nextIntervalTime.IsZero()) {
					e := hls.SendWatching()
					if e != nil {
						fmt.Println(e)
						return
					}
				}

			case <- chEndMain:
				return
			}
		}
	}()

	go func() {
		//fmt.Println("Entering goroutine#Main")
		defer func() {
			hls.wsConn.Close()
			close(chEndMain)
			//fmt.Println("goroutine#Main End")
		}()
		for {
			select {
				case <- chEndGo0:
					return
				case <- chEndGo1:
					return
				case <- chTimeout:
					return
				default:
			}
			var res interface{}
			err = hls.wsConn.ReadJSON(&res)
			if err != nil {
				fmt.Printf("%#v\n", err)
				return
			}

			//fmt.Printf("ReadJSON => %v\n", res)
			_type, ok := obj.FindString(res, "type")
			if (! ok) {
				fmt.Printf("type not found\n")
				continue
			}
			switch _type {
			case "watch":
				cmd, ok := obj.FindString(res, "body", "command")
				if ok {
					switch cmd {
					case "watchinginterval":
						if arr, ok := obj.FindArray(res, "body", "params"); ok {
							for _, intf := range arr {
								if str, ok := intf.(string); ok {
									num, e := strconv.Atoi(str)
									if e == nil {
										hls.SetInterval(num)
									}
								}
							}
						}
					case "currentstream":
						if uri, ok := obj.FindString(res, "body", "currentStream", "uri"); ok {
							//fmt.Printf("\n\n\n%s\n\n\n", uri)
							chUrl <- uri
						}
					case "disconnect":
						// print params
						if arr, ok := obj.FindArray(res, "body", "params"); ok {
							fmt.Printf("%v\n", arr)
						}
						chDone <-true
						return

					case "currentroom":
					case "statistics":
					case "permit":
					case "servertime":
					case "schedule":
						// nop
					default:
						fmt.Printf("%#v\n", res)
						fmt.Printf("unknown command: %s\n", cmd)
					} // end switch "command"

					break
				}

			case "ping":
				err = hls.SendPong()
				if err != nil {
					return
				}
			} // end switch "type"

		} // for ReadJSON
	}()

	<-chEndGo0
	<-chEndGo1
	<-chEndMain

	LB: for {
		select{
		case t := <-ch403:
			if t {
				//fmt.Println("detect 403")
				shouldReconnect = true
			}
		case t := <-chDone:
			if t {
				//fmt.Println("detect done")
				done = true
			}
		default:
			break LB
		}
	}

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

	hls, err := NewHls(broadcastId, webSocketUrl)
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

	for {
		shouldReconnect, d, e := hls.Wait(opt.NicoTestTimeout)
		if d {
			done = true
			break
		}
		if shouldReconnect {
			if e := hls.Connect(); e != nil {
				err = e
				return
			}
			continue
		}
		if e != nil {
			err = e
			return
		}
		break
	}

	return
}
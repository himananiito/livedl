package nicocas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"../../httpcommon"
	"../../objs"
	"../nicocasprop"
	"../nicodb"
	"../nicoprop"
	sqlite3 "github.com/mattn/go-sqlite3"
)

const xFrontendID = "91"

// NicoCasWork
type NicoCasWork struct {
	id     string
	ctx    context.Context
	cancel func()

	chError chan interface{}
	//chUnrecoverable chan error

	chPlaylistRequest chan playlistRequest
	//chStreamServer    chan streamServer

	masterPlaylist *url.URL

	chHeartbeat           chan putHeartbeat
	startPositionEn       bool
	startPosition         float64
	startPositionEnSecond bool
	closed                chan struct{}
	closeNotify           chan string
	chMedia               chan media

	mediaStatus sync.Map

	httpQueue chan httpcommon.HttpWork

	useArchive bool

	db *nicodb.NicoDB

	_actionTrackID atomic.Value
	_userSession   atomic.Value

	// medialoop
	mediaLoopClosed chan bool
	closeMediaLoop  chan bool
	mtxMediaLoop    sync.Mutex

	// アーカイブ高速DL時の待ち時間：秒
	archiveWait float64

	property property

	_seqNo          atomic.Value
	_position       atomic.Value
	_treamDuration  atomic.Value
	processingMedia sync.Map
	playlistDone    chan struct{}
	hbBreak         chan struct{}
}

func (w *NicoCasWork) setSeqNo(no uint64) {
	w._seqNo.Store(no)
}
func (w *NicoCasWork) getSeqNo() uint64 {
	val := w._seqNo.Load()
	switch v := val.(type) {
	case uint64:
		return v
	default:
		return 0
	}
}

func (w *NicoCasWork) setPosition(pos float64) {
	w._position.Store(pos)
}
func (w *NicoCasWork) getPosition() float64 {
	val := w._position.Load()
	switch v := val.(type) {
	case float64:
		return v
	default:
		return 0
	}
}

func (w *NicoCasWork) setStreamDuration(pos float64) {
	w._treamDuration.Store(pos)
}
func (w *NicoCasWork) getStreamDuration() float64 {
	val := w._treamDuration.Load()
	switch v := val.(type) {
	case float64:
		return v
	default:
		return 0
	}
}

// GetWorkerID returns
func GetWorkerID(id string) string {
	return fmt.Sprintf("nico:%s", id)
}

func (w *NicoCasWork) GetWorkerID() string {
	return GetWorkerID(w.property.GetID())
}

// GetID returns 放送ID
func (w *NicoCasWork) GetID() string {
	return w.property.GetID()
}

func (w *NicoCasWork) GetTitle() string {
	return w.property.GetTitle()
}

func (w *NicoCasWork) GetName() string {
	return w.property.GetName()
}

func (w *NicoCasWork) GetProgress() string {

	var s string
	if w.useArchive {
		pos := w.getPosition()
		streamPos := w.getStreamDuration()

		var percent float64
		if streamPos == 0 {
			percent = 0
		} else {
			percent = (pos / streamPos) * 100
		}

		pos_t := time.Date(2018, time.January, 1, 0, 0, int(pos), 0, time.UTC)
		spos_t := time.Date(2018, time.January, 1, 0, 0, int(streamPos), 0, time.UTC)

		pos_s := pos_t.Format("15:04:05")
		spos_s := spos_t.Format("15:04:05")
		s = fmt.Sprintf("%s / %s (%.2f%%)", pos_s, spos_s, percent)

		//s = fmt.Sprintf("%.2f / %.2f (%.2f%%)", pos_t.Format(""), streamPos, percent)

	} else {
		seqNo := w.getSeqNo()
		s = fmt.Sprintf("SeqNo=%d", seqNo)
	}

	return s
}

func (w *NicoCasWork) setActionTrackID(s string) {
	w._actionTrackID.Store(s)
}
func (w *NicoCasWork) getActionTrackID() string {
	return w._actionTrackID.Load().(string)
}

func (w *NicoCasWork) setUserSession(s string) {
	w._userSession.Store(s)
}
func (w *NicoCasWork) getUserSession() string {
	return w._userSession.Load().(string)
}

func (w *NicoCasWork) newRequest(method, uri string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, uri, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(w.ctx)
	return req, nil
}

// Close コンテキストキャンセル、終了チャンネルを閉じる
func (w NicoCasWork) Close() {
	select {
	case <-w.closed:
	default:
		w.cancel() // ctx
		w.dbClose()
		close(w.closed)
		w.closeNotify <- w.GetWorkerID()
	}
}

func (w NicoCasWork) dbClose() {

	select {
	case <-w.mediaLoopClosed:
		// mediaLoop already closed
	default:
		select {
		case w.closeMediaLoop <- true:
			select {
			case <-w.mediaLoopClosed:
			case <-time.After(10 * time.Second):
			}
		default:
		}
	}

	if w.db != nil {
		w.db.Close()
	}
}

// エラーを集約する
func (w *NicoCasWork) launchErrorHandler() {
	go func() {
		for {
			select {
			case err := <-w.chError:
				switch e := err.(type) {
				case postWatchingError:

					fmt.Printf("got postWatchingError %+v\n", e)
					switch e.ErrorCode {
					case "NOT_PLAYABLE":
						w.Close()
					}
					w.Close()

				case playlistError:
					fmt.Printf("got playlistError: %+v\n", e)

					if e.retry {
						time.Sleep(time.Duration(e.retryDelayMs) * time.Millisecond)
						fmt.Printf("playlistError 1")

						w.dbClose()
						fmt.Printf("playlistError 2")
						go w.start()
					}

				case sqlite3.Error:
					w.Close()
					return

				default:
					fmt.Printf("got error %#v\n", e)
				}
			case <-w.closed:
				return
			}
		}
	}()
}

func (w *NicoCasWork) addHTTPRequest(req *http.Request, cb httpcommon.Callback) {
	w.addHTTPRequestOption(req, cb, nil)
}
func (w *NicoCasWork) addHTTPRequestOption(req *http.Request, cb httpcommon.Callback, opt interface{}) {
	w.httpQueue <- httpcommon.HttpWork{
		QueuedAt: time.Now(),
		Client:   http.DefaultClient,
		Request:  req,
		Callback: cb,
		This:     w,
		Option:   opt,
	}
}

func (w *NicoCasWork) launchPlaylistLoop() {
	go w.playlistLoop()
}

func (w *NicoCasWork) launchHeartbeatLoop() {
	go w.heartbeatLoop()
}

func newNicoCasWork(parent context.Context, q chan httpcommon.HttpWork, closeNotify chan string,
	prop property, useArchive, startPositionEn bool, startPosition float64, archiveWait float64, userSession, actionTrackID string) *NicoCasWork {

	ctx, cancel := context.WithCancel(parent)

	chError := make(chan interface{}, 100)
	chPlaylistRequest := make(chan playlistRequest, 10)
	//chStreamServer := make(chan streamServer)
	chHeartbeat := make(chan putHeartbeat, 10)
	closed := make(chan struct{})
	chMedia := make(chan media, 100)
	mediaLoopClosed := make(chan bool, 0)
	closeMediaLoop := make(chan bool, 0)
	playlistDone := make(chan struct{})
	hbBreak := make(chan struct{})
	w := &NicoCasWork{
		ctx:               ctx,
		cancel:            cancel,
		chError:           chError,
		chPlaylistRequest: chPlaylistRequest,
		id:                prop.GetID(),
		chHeartbeat:       chHeartbeat,
		startPositionEn:   startPositionEn,
		startPosition:     startPosition,
		closed:            closed,
		closeNotify:       closeNotify,
		chMedia:           chMedia,
		mediaStatus:       sync.Map{},
		processingMedia:   sync.Map{},
		httpQueue:         q,
		useArchive:        useArchive,
		mediaLoopClosed:   mediaLoopClosed,
		closeMediaLoop:    closeMediaLoop,
		archiveWait:       archiveWait,
		property:          prop,
		playlistDone:      playlistDone,
		hbBreak:           hbBreak,
	}
	w.setActionTrackID(actionTrackID)
	w.setUserSession(userSession)

	w.launchErrorHandler()
	w.launchPlaylistLoop()
	w.launchHeartbeatLoop()

	return w
}

func (w *NicoCasWork) openDB() (err error) {

	var dbType int
	if w.useArchive {
		dbType = dbTypeCasArchive
	} else {
		dbType = dbTypeCasLive
	}

	var suffix string
	switch dbType {
	case dbTypeCasLive:
		suffix = "cas.sqlite3"
	case dbTypeCasArchive:
		suffix = "cas-archive.sqlite3"
	default:
		suffix = "sqlite3"
	}
	name := fmt.Sprintf("%v.%v", w.id, suffix)

	db, err := nicodb.Open(w.ctx, name)
	if err != nil {
		fmt.Println(err)
		return
	}
	w.db = db

	w.launchMediaLoop()

	return
}

func (w *NicoCasWork) getNextPosition() float64 {
	return w.db.GetNextPosition(w.ctx)
}
func (w *NicoCasWork) findBySeqNo(seqNo uint64) bool {
	return w.db.FindBySeqNo(w.ctx, seqNo)
}

func (w *NicoCasWork) start() {
	var uri string
	var postOpt postWatchingOption

	fmt.Println("start")
	// 必ず最初にDBを開かないと落ちる
	if err := w.openDB(); err != nil {
		w.chError <- err
		return
	}
	fmt.Println("done openDB")

	if w.useArchive {
		uri = fmt.Sprintf("https://api.cas.nicovideo.jp/v1/services/live/programs/%s/watching-archive", w.id)

		if w.startPositionEn {
			postOpt.positionEn = true

			pos := w.getNextPosition()
			fmt.Printf("\n\ngetNextPosition: %v\n\n", pos)
			if !w.startPositionEnSecond {
				w.startPositionEnSecond = true
				if pos > w.startPosition {
					postOpt.position = pos
				} else {
					postOpt.position = w.startPosition
				}
			} else {
				postOpt.position = pos
			}
		}

	} else {
		uri = fmt.Sprintf("https://api.cas.nicovideo.jp/v1/services/live/programs/%s/watching", w.id)
	}

	fmt.Println(uri)

	dat, _ := json.Marshal(map[string]interface{}{
		"actionTrackId":      w.getActionTrackID(),
		"isBroadcaster":      false,
		"isLowLatencyStream": false,
		"streamCapacity":     "superhigh",
		"streamProtocol":     "https",
		"streamQuality":      "auto",
	})

	req, _ := w.newRequest("POST", uri, bytes.NewBuffer(dat))
	userSession := w.getUserSession()
	req.Header.Set("Cookie", "user_session="+userSession)
	req.Header.Set("X-Frontend-Id", xFrontendID)
	req.Header.Set("X-Connection-Environment", "ethernet")
	req.Header.Set("Content-Type", "application/json")

	w.addHTTPRequestOption(req, cbPostWatching, postOpt)
}

const (
	dbTypeCasLive = iota
	dbTypeCasArchive
)

func (w *NicoCasWork) launchMediaLoop() {
	go w.mediaLoop()
}

type property interface {
	GetID() string
	GetTitle() string
	GetName() string
}

func Create(parent context.Context, q chan httpcommon.HttpWork, closeNotify chan string,
	prop property, useArchive, startPositionEn bool, startPosition, archiveWait float64, userSession, actionTrackID string) *NicoCasWork {

	work := newNicoCasWork(parent, q, closeNotify, prop, useArchive, startPositionEn, startPosition, archiveWait, userSession, actionTrackID)
	go work.start()

	return work
}

func GetProps(ctx context.Context, id, userSession string) (props property, err error) {
	uri := fmt.Sprintf("https://live.nicovideo.jp/watch/%s", id)

	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return
	}
	req = req.WithContext(ctx)

	req.Header.Set("Cookie", "user_session="+userSession)

	client := httpcommon.GetClient()

	res, err := client.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()

	dat, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}
	res.Body.Close()

	if ma := regexp.MustCompile(`data-props="(.+?)"`).FindSubmatch(dat); len(ma) > 0 {
		str := html.UnescapeString(string(ma[1]))

		switch req.URL.Host {
		case "live2.nicovideo.jp":
			p := nicoprop.NicoProperty{}

			//var p2 interface{}
			if err = json.Unmarshal([]byte(str), &p); err != nil {
				return
			}

			objs.PrintAsJson(p)

			props = p
		case "cas.nicovideo.jp":
			p := nicocasprop.NicocasProperty{}
			if err = json.Unmarshal([]byte(str), &p); err != nil {
				return
			}
			props = p
		default:
			err = fmt.Errorf("対応していません:%s", req.URL)
			return
		}

		objs.PrintAsJson(props)

	} else {
		err = fmt.Errorf("対応していません:%s", req.URL)
		return
	}

	return
}

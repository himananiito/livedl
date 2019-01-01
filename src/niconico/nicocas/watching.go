package nicocas

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

type putHeartbeat struct {
	expire int64
	uri    string
}

type messageServer struct {
	HTTP            string `json:"http"`
	HTTPS           string `json:"https"`
	ProtocolVersion int64  `json:"protocolVersion"` // 20061206
	Service         string `json:"service"`         // LIVE
	URL             string `json:"url"`             // xmlsocket://msg101.live.nicovideo.jp:xxxx/xxxx
	Version         int64  `json:"version"`
	Ws              string `json:"ws"`
	Wss             string `json:"wss"`
}
type playConfig struct {
	//
}

type timeCommon struct {
	BeginAt string `json:"beginAt"`
	EndAt   string `json:"endAt"`
}
type streamServer struct {
	Status  string `json:"status"`
	SyncURL string `json:"syncUrl"`
	URL     string `json:"url"`
}
type keys struct {
	ChatThreadKey    string `json:"chatThreadKey"`
	ControlThreadKey string `json:"controlThreadKey"`
	StoreThreadKey   string `json:"storeThreadKey"`
}
type threads struct {
	Chat    string `json:"chat"`
	Control string `json:"control"`
	Keys    keys   `json:"keys"`
	Store   string `json:"store"`
}
type program struct {
	Description string     `json:"description"`
	ID          string     `json:"id"` // lvxxxxs
	OnAirTime   timeCommon `json:"onAirTime"`
	ShowTime    timeCommon `json:"showTime"`
	Title       string     `json:"title"`
}
type data struct {
	ExpireIn         int64         `json:"expireIn"`
	MessageServer    messageServer `json:"messageServer"`
	PlayConfig       playConfig    `json:"playConfig"`
	PlayConfigStatus string        `json:"playConfigStatus"` // "ready"
	Program          program       `json:"program"`
	StreamServer     streamServer  `json:"streamServer"`
	Threads          threads       `json:"threads"`
}

type meta struct {
	Status       int64  `json:"status"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}
type watchingResponse struct {
	Data data `json:"data"`
	Meta meta `json:"meta"`
}

type dataPut struct {
	ExpireIn     int64        `json:"expireIn"`
	StreamServer streamServer `json:"streamServer"`
}
type putResponse struct {
	Meta    meta    `json:"meta"`
	DataPut dataPut `json:"data"`
}

type postWatchingError struct {
	Error        error
	Status       int64
	ErrorCode    string
	ErrorMessage string
}

type postWatchingOption struct {
	positionEn bool    // 位置指定有効
	position   float64 // 位置の値
}

func cbPostWatching(res *http.Response, err error, this, opt interface{}, queuedAt, startedAt time.Time) {
	//fmt.Println("cbPostWatching!")
	//defer fmt.Println("cbPostWatching done!")
	w := this.(*NicoCasWork)
	if err != nil {
		w.chError <- postWatchingError{Error: err}
		return
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		w.chError <- postWatchingError{Error: err}
		return
	}

	var response watchingResponse
	fmt.Printf("%s\n", string(bs))
	json.Unmarshal(bs, &response)

	//objs.PrintAsJson(response)

	switch response.Meta.Status {
	case 201:
		if response.Data.StreamServer.URL != "" {
			optPos := opt.(postWatchingOption)
			if w.useArchive {
				if optPos.positionEn {
					master := createMasterURLWithPosition(response.Data.StreamServer.URL, optPos.position)
					w.addPlaylistRequest(master, 0, playlistTypeUnknown, optPos.positionEn, response.Data.StreamServer.URL, 0)
				} else {
					w.addPlaylistRequest(response.Data.StreamServer.URL, 0, playlistTypeUnknown, optPos.positionEn, response.Data.StreamServer.URL, 0)
				}

			} else {
				w.addPlaylistRequest(response.Data.StreamServer.URL, 0, playlistTypeUnknown, optPos.positionEn, response.Data.StreamServer.URL, 0)
			}

		} else {
			w.chError <- postWatchingError{Error: errors.New("StreamServer.URL is null")}
		}
	default:
		w.chError <- postWatchingError{
			Error:        errors.New("meta.Status Not OK"),
			Status:       response.Meta.Status,
			ErrorCode:    response.Meta.ErrorCode,
			ErrorMessage: response.Meta.ErrorMessage,
		}
	}

	// heartbeat
	w.putRequest(response.Data.ExpireIn, res.Request.URL.String())
}

func cbPutWatching(res *http.Response, err error, this, _ interface{}, queuedAt, startedAt time.Time) {
	w := this.(*NicoCasWork)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	obj := putResponse{}
	err = json.Unmarshal(bs, &obj)
	if err != nil {
		fmt.Println(err)
		return
	}

	w.putRequest(obj.DataPut.ExpireIn, res.Request.URL.String())
}

func (w *NicoCasWork) putRequest(expire int64, uri string) {
	if expire > 0 {

		select {
		case w.hbBreak <- struct{}{}:
		default:
		}

		hb := putHeartbeat{
			expire: expire,
			uri:    uri,
		}

		select {
		case w.chHeartbeat <- hb:
		case <-w.closed:
			return
		}
	}
}

func (w *NicoCasWork) heartbeatLoop() {
	for {
		select {
		case hb := <-w.chHeartbeat:
			if hb.expire > 0 {
				fmt.Printf("putRequest %#v\n", hb)
				select {
				case <-time.After(time.Duration(hb.expire) * time.Millisecond):
					dat, _ := json.Marshal(map[string]interface{}{
						"actionTrackId": w.getActionTrackID(),
						"isBroadcaster": false,
					})
					req, _ := http.NewRequest("PUT", hb.uri, bytes.NewBuffer(dat))
					req.Header.Set("Content-Type", "application/json")
					userSession := w.getUserSession()
					req.Header.Set("Cookie", "user_session="+userSession)
					req.Header.Set("X-Frontend-Id", xFrontendID)

					w.addHTTPRequest(req, cbPutWatching)
				case <-w.hbBreak:
				labelHbBreak:
					for {
						select {
						case <-w.chHeartbeat:
						default:
							break labelHbBreak
						}
					}
					break
				case <-w.closed:
					return
				}
			}
		case <-w.closed:
			return
		}
	}
}

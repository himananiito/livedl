package nicocas

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/himananiito/m3u8"
)

const (
	playlistTypeUnknown = iota
	playlistTypeMaster
	playlistTypeMedia
)

type playlistRequest struct {
	waitSecond   float64
	uri          string
	playlistType int
	useArchive   bool
	//fastQueueing bool
	//positionEn   bool
	//masterURL    string
	//bandwidth    int64
	playlistOption
}

type playlistError struct {
	error
	retry        bool
	retryDelayMs int64
}

type playlistOption struct {
	fastQueueing bool
	positionEn   bool
	masterURL    string
	bandwidth    int64
}

func cbPlaylist(res *http.Response, err error, this, opt interface{}, queuedAt, startedAt time.Time) {
	w := this.(*NicoCasWork)
	playlistOpt := opt.(playlistOption)
	//defer fmt.Println("done cbPlaylist")
	if err != nil {
		w.chError <- playlistError{error: err}
		return
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case 200:
	default:
		var retry bool
		if res.StatusCode != 404 {
			retry = true
		}

		w.chError <- playlistError{
			error:        fmt.Errorf("StatusCode is %v", res.StatusCode),
			retry:        retry,
			retryDelayMs: 1000,
		}
		return
	}

	playlist, listType, err := m3u8.DecodeFrom(res.Body, true)
	if err != nil {
		w.chError <- playlistError{error: err}
		return
	}

	switch listType {
	case m3u8.MEDIA:
		w.handleMediaPlaylist(playlist.(*m3u8.MediaPlaylist), res.Request.URL, playlistOpt)

	case m3u8.MASTER:
		w.handleMasterPlaylist(playlist.(*m3u8.MasterPlaylist), res.Request.URL, playlistOpt)
	}
}

// createMasterURLWithPosition MasterプレイリストのURLを指定した開始時間付きの文字列で返す
func createMasterURLWithPosition(URL string, currentPos float64) string {
	masterURL, _ := url.Parse(URL)
	query := masterURL.Query()
	query.Set("start", fmt.Sprintf("%f", currentPos))
	var format string
	if strings.HasPrefix(masterURL.Path, "/") {
		format = "%s://%s%s?%s"
	} else {
		format = "%s://%s/%s?%s"
	}

	return fmt.Sprintf(
		format,
		masterURL.Scheme,
		masterURL.Host,
		masterURL.Path,
		query.Encode(),
	)
}

func (w *NicoCasWork) handleMasterPlaylist(masterpl *m3u8.MasterPlaylist, URL *url.URL, opt playlistOption) {
	fmt.Printf("%+v\n", masterpl)

	bws := make([]int, 0)
	playlists := map[int]*m3u8.Variant{}
	for _, variant := range masterpl.Variants {
		if variant == nil {
			break
		}

		bw := int(variant.Bandwidth)
		bws = append(bws, bw)
		playlists[bw] = variant
	}

	if len(bws) == 0 {
		fmt.Println("No playlist")
		w.chError <- playlistError{
			error: fmt.Errorf("No playlist in master"),
		}
		return
	}

	limit := 999999999
	sort.Ints(bws)

	var selected int
	for _, bw := range bws {
		if bw > limit {
			break
		}
		selected = bw
	}

	// 全てが制限値以上のBWの場合は一番小さいものを選ぶ
	if selected == 0 {
		selected = bws[0]
	}

	pl, _ := playlists[selected]

	uri, err := url.Parse(pl.URI)
	if err != nil {
		panic(err)
	}

	streamURL := URL.ResolveReference(uri).String()

	//fmt.Printf("handleMasterPlaylist    =>    %v\n", URL.String())
	w.addPlaylistRequest(streamURL, 0, playlistTypeMaster, opt.positionEn, URL.String(), int64(selected))
}

func (w *NicoCasWork) handleMediaPlaylist(mediapl *m3u8.MediaPlaylist, uri *url.URL, opt playlistOption) {

	fmt.Printf("%+v\n", opt)
	fmt.Printf("%+v\n", mediapl)

	w.setSeqNo(mediapl.SeqNo)

	var pos float64
	var posDefined bool
	if mediapl.CurrentPosition != nil {
		pos = *mediapl.CurrentPosition
		posDefined = true
	} else if mediapl.DMCCurrentPosition != nil {
		pos = *mediapl.DMCCurrentPosition
		posDefined = true
	}

	var streamDuration float64
	var streamDurationDefined bool
	if mediapl.DMCStreamDuration != nil {
		streamDuration = *mediapl.DMCStreamDuration
		streamDurationDefined = true
	} else if mediapl.StreamDuration != nil {
		streamDuration = *mediapl.StreamDuration
		streamDurationDefined = true
	}

	var registered int
	var totalDuration float64
	for i, seg := range mediapl.Segments {
		if seg == nil {
			mediapl.Segments = mediapl.Segments[:i]
			break
		}

		if i == 0 && seg.Duration > 0 && !w.useArchive && !opt.fastQueueing {
			fmt.Println("add fast queueing!!!!!!!!")
			// 一番最初だけ実行される
			w.addPlaylistRequestFast(uri.String(), seg.Duration, playlistTypeMedia, opt.masterURL, opt.bandwidth)
		}

		seqNo := mediapl.SeqNo + uint64(i)

		if posDefined {
			pos += seg.Duration
		}
		totalDuration += seg.Duration

		b, e := url.Parse(seg.URI)
		if e != nil {
			panic(e)
		}

		req, _ := w.newRequest("GET", uri.ResolveReference(b).String(), nil)

		var posActual float64
		if posDefined {
			posActual = pos
		} else {
			posActual = 0
		}
		if w.downloadMedia(req, seqNo, seg.Duration, posActual, opt.bandwidth) {
			registered++
		}
	}

	var diff float64
	if posDefined && streamDurationDefined {
		diff = streamDuration - pos
		fmt.Printf("DIFF is %v\n", diff)
	}

	if posDefined {
		w.setPosition(pos)
	}
	if streamDurationDefined {
		w.setStreamDuration(streamDuration)
	}

	if mediapl.Closed {
		// #EXT-X-ENDLIST
		select {
		case w.playlistDone <- struct{}{}:
		default:
		}

		return
	}

	// アーカイブの場合はここで次のHLS取得設定する
	if w.useArchive && !opt.fastQueueing {
		var playlistURL string
		var wait float64

		var masterURL = opt.masterURL

		if opt.positionEn && posDefined {
			// 位置移動する場合
			playlistURL = createMasterURLWithPosition(opt.masterURL, pos)

			masterURL = playlistURL

			fmt.Printf("\n%v -->\n%v\n\n", opt.masterURL, playlistURL)

			fmt.Printf("registered=%v totalDuration=%v\n", registered, totalDuration)
			if diff < totalDuration {
				playlistURL = uri.String()
				if diff > mediapl.Segments[0].Duration {
					wait = mediapl.Segments[0].Duration
				} else {
					wait = 5.001
				}
			} else if registered > 1 {

				wait = w.archiveWait

			} else {
				if len(mediapl.Segments) > 0 {
					if diff > mediapl.Segments[0].Duration {

						fmt.Println("yarinaosi")

						w.chError <- playlistError{
							error:        errors.New("retry playlist"),
							retry:        true,
							retryDelayMs: 1000,
						}

						return

					} else {
						wait = mediapl.Segments[0].Duration
						fmt.Printf("wait: if, 1 \n")
					}

				} else {
					wait = 5.01
					fmt.Printf("wait: if, 2 \n")
				}
				playlistURL = uri.String()
			}

		} else {
			// 位置移動しない
			playlistURL = uri.String()

			if len(mediapl.Segments) > 0 {
				wait = mediapl.Segments[0].Duration
				fmt.Printf("wait: else, 1 \n")
			} else {
				wait = 5.01
			}
		}

		fmt.Printf("wait is %v\n", wait)
		w.addPlaylistRequest(playlistURL, wait, playlistTypeMaster, opt.positionEn, masterURL, opt.bandwidth)
	}
}

func (w *NicoCasWork) addPlaylistRequest(uri string, waitSecond float64, playlistType int, positionEn bool, masterURL string, bandwidth int64) {
	opt := playlistOption{
		fastQueueing: false,
		positionEn:   positionEn,
		masterURL:    masterURL,
		bandwidth:    bandwidth,
	}
	w.addPlaylistRequestCommon(uri, waitSecond, playlistType, opt)
}
func (w *NicoCasWork) addPlaylistRequestFast(uri string, waitSecond float64, playlistType int, masterURL string, bandwidth int64) {
	opt := playlistOption{
		fastQueueing: true,
		positionEn:   false,
		masterURL:    masterURL,
		bandwidth:    bandwidth,
	}
	w.addPlaylistRequestCommon(uri, waitSecond, playlistType, opt)
}
func (w *NicoCasWork) addPlaylistRequestCommon(uri string, waitSecond float64, playlistType int, opt playlistOption) {
	w.chPlaylistRequest <- playlistRequest{
		uri:            uri,
		waitSecond:     waitSecond,
		playlistType:   playlistType,
		useArchive:     w.useArchive,
		playlistOption: opt,
	}
}

func (w *NicoCasWork) downloadMedia(req *http.Request, seqNo uint64, duration, position float64, bandwidth int64) bool {
	if w.findBySeqNo(seqNo) {
		w.mediaStatus.Store(seqNo, true)
		return false
	}

	if _, ok := w.mediaStatus.Load(seqNo); !ok {
		w.mediaStatus.Store(seqNo, false)
		w.processingMedia.Store(seqNo, true)
		chunkOpt := mediaChunkOption{
			seqNo:     seqNo,
			duration:  duration,
			position:  position,
			bandwidth: bandwidth,
		}
		w.addHTTPRequestOption(req, cbMediaChunk, chunkOpt)
		return true
	}
	return false
}

func (w *NicoCasWork) playlistLoop() {
	for {
		select {
		case preq := <-w.chPlaylistRequest:
			go func(preq playlistRequest) {
				//fmt.Printf("sleep duration: %+v\n", preq.waitSecond)
				select {
				case <-time.After(time.Duration(preq.waitSecond * float64(time.Second))):
				case <-w.closed:
					return
				}

				req, _ := w.newRequest("GET", preq.uri, nil)

				w.addHTTPRequestOption(req, cbPlaylist, preq.playlistOption)

				// Liveの場合
				// レスポンスを待たずに次のプレイリクエストをキューイング
				if preq.fastQueueing && !preq.useArchive && preq.playlistType == playlistTypeMedia {
					w.addPlaylistRequestFast(preq.uri, preq.waitSecond, preq.playlistType, preq.masterURL, preq.bandwidth)
				}

			}(preq)

		case <-w.playlistDone:
			fmt.Println("got playlistDone")

			for i := 0; i < 60; i++ {

				// 完了を待つ
				var wait bool
				w.processingMedia.Range(func(k, v interface{}) bool {
					wait = true
					return false
				})

				if wait {
					select {
					case <-time.After(time.Second):
					case <-w.closed:
						return
					}
				} else {
					w.Close()

					// TODO: 変換キューに積む

					return
				}
			}

			return
		case <-w.closed:
			return
		}
	}
}

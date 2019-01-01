package nicocas

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

type media struct {
	seqNo     uint64
	duration  float64
	position  float64 // 現在の再生時刻
	bandwidth int64
	size      int64
	data      []byte
}

type mediaReadError struct {
	error
}

type mediaChunkOption struct {
	seqNo     uint64
	duration  float64
	position  float64
	bandwidth int64
}

func cbMediaChunk(res *http.Response, err error, this, opt interface{}, queuedAt, startedAt time.Time) {
	w := this.(*NicoCasWork)
	chunkOpt := opt.(mediaChunkOption)

	var ok bool
	var is404 bool
	defer func() {
		if ok {
			w.mediaStatus.Store(chunkOpt.seqNo, true)
		} else if is404 {
			w.processingMedia.Delete(chunkOpt.seqNo)
			w.mediaStatus.Store(chunkOpt.seqNo, true)
		} else {
			w.processingMedia.Delete(chunkOpt.seqNo)
			w.mediaStatus.Delete(chunkOpt.seqNo)
		}
	}()

	if err != nil {
		w.chError <- mediaReadError{err}
		return
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case 200:
	default:
		if res.StatusCode == 404 {
			is404 = true
		}
		w.chError <- mediaReadError{fmt.Errorf("StatusCode is %v: %v", res.StatusCode, res.Request.URL)}
		return
	}

	if res.ContentLength < 10*1024*1024 {
		bs, err := ioutil.ReadAll(res.Body)
		if err != nil {
			w.chError <- mediaReadError{err}
			return
		}

		if res.ContentLength == int64(len(bs)) {

			w.chMedia <- media{
				seqNo:     chunkOpt.seqNo,
				duration:  chunkOpt.duration,
				position:  chunkOpt.position,
				bandwidth: chunkOpt.bandwidth,
				size:      int64(len(bs)),
				data:      bs,
			}

			ok = true

		} else {
			w.chError <- mediaReadError{fmt.Errorf("read error: %v != %v", res.ContentLength, len(bs))}
		}
	} else {
		w.chError <- mediaReadError{fmt.Errorf("[FIXME] too large: %v", res.ContentLength)}
	}
}

func (w *NicoCasWork) saveMedia(seqNo uint64, position, duration float64, bandwidth, size int64, data []byte) error {
	return w.db.InsertMedia(seqNo, position, duration, bandwidth, size, data)
}

// チャンネルからシーケンスを受け取ってDBに入れていく
func (w *NicoCasWork) mediaLoop() {
	// this is guard
	w.mtxMediaLoop.Lock()
	defer w.mtxMediaLoop.Unlock()

	defer func() {
		fmt.Printf("Closing mediaLoop\n")
		select {
		case w.mediaLoopClosed <- true:
		case <-time.After(10 * time.Second):
			fmt.Println("[FIXME] Closing mediaLoop")
		}
	}()

	for {
		select {
		case media := <-w.chMedia:
			fmt.Printf("inserting DB %v %v %v %v %v\n", media.seqNo, media.duration, media.position, media.size, media.bandwidth)

			err := w.saveMedia(media.seqNo, media.position, media.duration, media.bandwidth, media.size, media.data)
			w.processingMedia.Delete(media.seqNo)
			if err != nil {
				fmt.Println(err)
				return
			}

		case <-w.closeMediaLoop:
			return

		case <-w.closed:
			return
		}
	}
}

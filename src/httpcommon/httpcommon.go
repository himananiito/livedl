package httpcommon

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Callback func(*http.Response, error, interface{}, interface{}, time.Time, time.Time)

type HttpWork struct {
	Client   *http.Client
	Request  *http.Request
	Callback Callback
	This     interface{}
	QueuedAt time.Time
	Option   interface{}
}

func GetClient() *http.Client {
	var client = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) (err error) {
			if req != nil && via != nil && len(via) > 0 {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}

				// 元のRequest.URLでリダイレクト後のURLを取れるようにしたい
				via[0].URL = req.URL

				// ニコニコならCookieを引き継ぐ
				if strings.HasSuffix(req.URL.Host, ".nicovideo.jp") {
					req.Header = via[0].Header
				}
			}
			return nil
		},
	}
	return client
}

func Launch(num int) (q chan HttpWork) {
	q = make(chan HttpWork, 10)
	var m sync.Mutex

	requestPerSec := 6.0                // [リクエスト数/秒] 超える場合に一定期間Sleepする
	sleepTime := 500 * time.Millisecond // Sleep時間
	arrSize := 5                        // サンプル数

	arr := make([]int64, 0, arrSize)

	checkLimit := func(t time.Time) {
		nano := t.UnixNano()
		m.Lock()
		defer m.Unlock()

		if len(arr) >= arrSize {
			arr = arr[1:arrSize]
			arr = append(arr, nano)

			diff := arr[arrSize-1] - arr[0]                                 // total sec
			delta := float64(len(arr)) / float64(diff) * 1000 * 1000 * 1000 // requests per sec

			//fmt.Printf("delta is %v\n", delta)
			if delta >= requestPerSec {
				arr = arr[:0]
				time.Sleep(sleepTime)
				//return true
			}
		} else {
			arr = append(arr, nano)
		}
		//return false
	}

	for i := 0; i < num; i++ {
		go func() {
			for htw := range q {

				startedAt := time.Now()

				checkLimit(startedAt)

				res, err := htw.Client.Do(htw.Request)
				htw.Callback(res, err, htw.This, htw.Option, htw.QueuedAt, startedAt)
			}
		}()
	}
	return q
}

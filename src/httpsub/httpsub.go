
package httpsub

import (
	"net/http"
	"os"
	"sync"
	"log"
	"io"
	"fmt"
	"bytes"
)

type SubDownloader struct {
	method string
	uri string
	data []byte
	Header map[string]string
	RangeSize int64
	BuffSize int64
	fileName string
	file *os.File
	numConcurrent int
	chRunning chan bool
	mtx sync.Mutex
	wg sync.WaitGroup
	chLength chan int64
}
func (sub *SubDownloader) Concurrent(c int) {
	sub.numConcurrent = c
}
func Get(uri, fileName string) (sub *SubDownloader) {
	sub = &SubDownloader{
		method: "GET",
		uri: uri,
		fileName: fileName,
	}
	return
}
func (sub *SubDownloader) Close() {
	sub.mtx.Lock()
	defer sub.mtx.Unlock()
	if sub.file != nil {
		sub.file.Close()
		sub.file = nil
	}
}
func (sub *SubDownloader) open() {
	f, err := os.Create(sub.fileName)
	if err != nil {
		log.Fatal(err)
	}
	sub.file = f
}
func (sub *SubDownloader) write(pos int64, rdr io.Reader) (err error) {
	sub.mtx.Lock()
	defer sub.mtx.Unlock()
	//fmt.Printf("write %d\n", pos)
	if sub.file == nil {
		sub.open()
	}
	if _, err = sub.file.Seek(pos, 0); err != nil {
		log.Fatalln(err)
	}
	if _, err = io.Copy(sub.file, rdr); err != nil {
		log.Fatalln(err)
	}
	return
}
func (sub *SubDownloader) subrange(pos int64) {
	//fmt.Printf("start subrange pos(%d), size(%d) \n", pos, sub.RangeSize)
	sub.wg.Add(1)
	sub.chRunning <- true
	go func() {
		defer func() {
			<-sub.chRunning
			sub.wg.Done()
		}()
		data := bytes.NewBuffer(sub.data)
		req, _ := http.NewRequest(sub.method, sub.uri, data)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", pos, pos + sub.RangeSize - 1))

		client := new(http.Client)
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case 206:
		default:
			log.Fatalf("StatusCode is %v\n", resp.StatusCode)
		}
		sub.chLength <- resp.ContentLength

		buff := new(bytes.Buffer)
		wbytes := int64(0)
		for {
			n, _ := io.CopyN(buff, resp.Body, sub.BuffSize)
			//fmt.Printf("buff size is %d\n", buff.Len())
			if n > 0 {
				sub.write(pos + wbytes, buff)
				wbytes += n
			} else {
				return
			}
		}
	}()
}
func (sub *SubDownloader) Wait() {
	sub.chRunning = make(chan bool, sub.numConcurrent)
	sub.chLength = make(chan int64, 10)

	if sub.RangeSize <= 0 {
		sub.RangeSize = 10*1000*1000
	}

	if sub.BuffSize <= 0 {
		sub.BuffSize = 3*1000*1000
	}

	pos := int64(0)
	for {
		sub.subrange(pos)
		length := <-sub.chLength
		fmt.Printf("Downloading %v: %v-%v\n", sub.fileName, pos, pos + length - 1)
		if length == sub.RangeSize {
			pos += length
		} else {
			break
		}
	}
	sub.wg.Wait()
	sub.Close()
}

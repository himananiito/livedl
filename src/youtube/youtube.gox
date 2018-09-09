package youtube

import (
	"fmt"
	"net/http"

	"io/ioutil"
	"regexp"
	"encoding/json"
	"html"
	"strings"
	"net/url"
	"os"
	"strconv"
	"bytes"
	"os/exec"
	"os/signal"
	"archive/zip"
	"sync"
	"../obj"
	"io"
	"../files"

	"../httpsub"
	"../zip2mp4"
	"log"
)

type YtDash struct {
	SeqNo int
	SeqNoFound bool
	SeqNoBack int
	VAddr string
	VQuery url.Values
	AAddr string
	AQuery url.Values
	TsFile *os.File
	FFCmd *exec.Cmd
	FFBuffer *bytes.Buffer
	TryBack bool
	StartBack bool
	ChEnd chan bool
	ChEndBack chan bool
	zipFile *os.File
	zipWriter *zip.Writer
	mZip sync.Mutex
	fileName string
	Title string
	Id string
}
func (yt *YtDash) SetFileName(fileName string) {
	yt.fileName = files.ReplaceForbidden(fileName)
}
func (yt *YtDash) fetch(isVideo, isBack bool) (fileName string, err error) {

	var addr string
	var query url.Values
	var sn int

	if isVideo {
		addr = yt.VAddr
		query = yt.VQuery
	} else {
		addr = yt.AAddr
		query = yt.AQuery
	}

	if isBack && (! yt.SeqNoFound) {
		err = fmt.Errorf("isBack && (! SeqNoFound)")
		return
	}

	if yt.SeqNoFound {
		//fmt.Printf("SQ set to %d\n", yt.SeqNo)
		if isBack {
			sn = yt.SeqNoBack
		} else {
			sn = yt.SeqNo
		}
		query.Set("sq", fmt.Sprintf("%d", sn))

		//fmt.Printf("%v\n", query)
	}

	uri := fmt.Sprintf("%s?%s", addr, query.Encode())
	req, _ := http.NewRequest("GET", uri, nil)

	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
	default:
		err = fmt.Errorf("StatusCode is %v\n%v\n%v", resp.StatusCode, uri, query)
		return
	}

	switch query.Get("source") {
	case "yt_live_broadcast":

		bs, e := ioutil.ReadAll(resp.Body)
		if e != nil {
			err = e
			return
		}

		if (! yt.SeqNoFound) && (! isBack) {

			if ma := regexp.MustCompile(`Sequence-Number\s*:\s*(\d+)`).FindSubmatch(bs); len(ma) > 0 {
				sn, err = strconv.Atoi(string(ma[1]))
				if err != nil {
					err = fmt.Errorf("Sequence-Number Not a Number: %v", ma)
					return
				}
				yt.SeqNo = sn
				yt.SeqNoBack = sn - 1
				yt.SeqNoFound = true
				fmt.Printf("start SeqNo: %d\n", sn)

			} else {
				err = fmt.Errorf("Sequence-Number Not found")
				return
			}

			yt.RecordBack()
		}

		if isVideo {
			fileName = fmt.Sprintf("video-%d.mp4", sn)
		} else {
			fileName = fmt.Sprintf("audio-%d.mp4", sn)
		}

		buff := bytes.NewBuffer(bs)
		if err = yt.WriteZip(fileName, buff); err != nil {
			return
		}
	}

	return
}
func (yt *YtDash) fetchVideo() (string, error) {
	return yt.fetch(true, false)
}
func (yt *YtDash) fetchAudio() (string, error) {
	return yt.fetch(false, false)
}
func (yt *YtDash) IncrSeqNo() {
	yt.SeqNo++
}
func (yt *YtDash) fetchVideoBack() (string, error) {
	return yt.fetch(true, true)
}
func (yt *YtDash) fetchAudioBack() (string, error) {
	return yt.fetch(false, true)
}
func (yt *YtDash) DecrSeqNoBack() {
	yt.SeqNoBack--
}
func (yt *YtDash) RecordYoutube() {
	var vname string
	var aname string
	func() {
		uri := fmt.Sprintf("%s?%s", yt.VAddr, yt.VQuery.Encode())
		vname = fmt.Sprintf("%s(%s)-v.mp4", yt.Title, yt.Id)
fmt.Println(uri)
		sub := httpsub.Get(uri, vname)
		sub.Concurrent(4)
		sub.Wait()
	}()
	func() {
		uri := fmt.Sprintf("%s?%s", yt.AAddr, yt.AQuery.Encode())
		aname = fmt.Sprintf("%s(%s)-a.mp4", yt.Title, yt.Id)
		sub := httpsub.Get(uri, aname)
		sub.Concurrent(4)
		sub.Wait()
	}()
	if zip2mp4.FFmpegExists() {
		exts := []string{"mp4", "mkv"}
		for _, ext := range exts {
			oname := fmt.Sprintf("%s(%s).%s", yt.Title, yt.Id, ext)
			if zip2mp4.MergeVA(vname, aname, oname) {
				os.Remove(vname)
				os.Remove(aname)
				return
			} else {
				os.Remove(oname)
			}
		}
	}
	// ffmpeg Not exists OR merge NG
	fv, e := os.Open(vname)
	if e != nil {
		log.Fatalln(e)
	}
	yt.WriteZip("video.mp4", fv)
	fv.Close()

	fa, e := os.Open(aname)
	if e != nil {
		log.Fatalln(e)
	}
	yt.WriteZip("audio.mp4", fa)
	fa.Close()

	os.Remove(vname)
	os.Remove(aname)

}
func (yt *YtDash) Wait() {

	yt.ChEnd = make(chan bool)
	yt.ChEndBack = make(chan bool)

	switch yt.VQuery.Get("source") {
	case "youtube":
		yt.RecordYoutube()

	case "yt_live_broadcast":
		yt.RecordForward()
		<-yt.ChEnd
		if yt.StartBack {
			<-yt.ChEndBack
		}
	}
}
func (yt *YtDash) Close() {
	if yt.zipWriter != nil {
		yt.zipWriter.Close()
	}
	if yt.zipFile != nil {
		yt.zipFile.Close()
	}
}
func (yt *YtDash) OpenFile() (err error) {

	fileName, err := files.GetFileNameNext(yt.fileName)
	if err != nil {
		return
	}

	file, err := os.Create(fileName)
	if err != nil {
		log.Fatalln(err)
	}
	yt.zipFile = file

	yt.zipWriter = zip.NewWriter(file)

	chSig := make(chan os.Signal, 1)
	signal.Notify(chSig, os.Interrupt)
	go func() {
		<-chSig
		yt.mZip.Lock()
		defer yt.mZip.Unlock()
		if yt.zipWriter != nil {
			yt.zipWriter.Close()
		}
		os.Exit(0)
	}()
	return
}

func (yt *YtDash) WriteZip(name string, rdr io.Reader) (err error) {
	yt.mZip.Lock()
	defer yt.mZip.Unlock()

	if yt.zipFile == nil || yt.zipWriter == nil {
		yt.OpenFile()
	}

	wr, err := yt.zipWriter.Create(name)
	if err != nil {
		return
	}

	if _, err = io.Copy(wr, rdr); err != nil {
		return
	}
	return
}
func (yt *YtDash) RecordForward() {
	go func() {
		defer func() {
			close(yt.ChEnd)
		}()
		for {
			vfile, err := yt.fetchVideo()
			if err != nil {
				fmt.Printf("RecordForward: %v\n", err)
				return
			}
			afile, err := yt.fetchAudio()
			if err != nil {
				fmt.Printf("RecordForward: %v\n", err)
				return
			}
			if true {
				fmt.Printf("%s, %s\n", vfile, afile)
			}
			yt.IncrSeqNo()
		}
	}()
}
func (yt *YtDash) RecordBack() {
	if yt.TryBack && (! yt.StartBack) {
		yt.StartBack = true
		go func() {
			defer func() {
				close(yt.ChEndBack)
			}()
			for yt.SeqNoBack >= 0 {
				vfile, err := yt.fetchVideoBack()
				if err != nil {
					fmt.Printf("RecordBack: %v\n", err)
					return
				}
				afile, err := yt.fetchAudioBack()
				if err != nil {
					fmt.Printf("RecordBack: %v\n", err)
					return
				}
				if true {
					fmt.Printf("%s, %s\n", vfile, afile)
				}
				yt.DecrSeqNoBack()
			}
		}()
	}
}

func Record(id string) (err error) {
	uri := fmt.Sprintf("https://www.youtube.com/watch?v=%s", id)
	req, _ := http.NewRequest("GET", uri, nil)

	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	dat, _ := ioutil.ReadAll(resp.Body)

	var a interface{}

	re := regexp.MustCompile(`\Wytplayer\.config\p{Zs}*=\p{Zs}*({.*?})\p{Zs}*;`)
	if ma := re.FindSubmatch(dat); len(ma) > 0 {
		str := html.UnescapeString(string(ma[1]))
		if err = json.Unmarshal([]byte(str), &a); err != nil {
			fmt.Println(str)
			fmt.Println(err)
			return
		}
	} else {
		fmt.Println("ytplayer.config not found")
		return
	}

	// debug print
	//obj.PrintAsJson(a)

	title, ok := obj.FindString(a, "args", "title")
	if (! ok) {
		fmt.Println("title not found")
		return
	}

	res, ok := obj.FindString(a, "args", "adaptive_fmts")
	if (! ok) {
		if res, ok := obj.FindString(a, "args", "hlsvp"); ok {
			fmt.Printf("hls: %s\n", res)
			return
		}
		obj.PrintAsJson(a)
		return
	}

	var maxVideoBr int
	var maxAudioBr int
	var videoUrl string
	var audioUrl string
	var qualityLabel string
	for _, s := range strings.Split(res, ",") {
		//fmt.Println(s)
		f, e := url.ParseQuery(s)
		//obj.PrintAsJson(f)
		//fmt.Println(f)
		if e != nil {
			fmt.Println(e)
			return
		}
		// type
		// bitrate
		t := f.Get("type")
		br, err := strconv.Atoi(f.Get("bitrate"))
		if err != nil {
			continue
		}

		if strings.HasPrefix(t, "video") {
			if br > maxVideoBr {
				maxVideoBr = br
				videoUrl = f.Get("url")
				qualityLabel = f.Get("quality_label")
			}
		} else if strings.HasPrefix(t, "audio") {
			if br > maxAudioBr {
				maxAudioBr = br
				audioUrl = f.Get("url")
			}
		}
	}
	fmt.Printf("Quality: %s\n", qualityLabel)

	varr := strings.SplitN(videoUrl, "?", 2)
	if len(varr) != 2 {
		return
	}
	aarr := strings.SplitN(audioUrl, "?", 2)
	if len(aarr) != 2 {
		return
	}

	yt := new(YtDash)
	defer yt.Close()

	yt.Id = id
	yt.Title = files.ReplaceForbidden(title)

	yt.SetFileName(fmt.Sprintf("%s(%s).zip", title, id))

	yt.VAddr = varr[0]
	vQuery, e := url.ParseQuery(varr[1])
	if e != nil {
		return
	}
	yt.VQuery = vQuery

	//obj.PrintAsJson(vQuery)
	//fmt.Println(yt.VAddr + "?" + vQuery.Encode())

	yt.AAddr = aarr[0]
	aQuery, e := url.ParseQuery(aarr[1])
	if e != nil {
		return
	}
	yt.AQuery = aQuery

	yt.TryBack = true
	yt.Wait()

	return
}

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/pprof"
	_ "net/http/pprof"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"./files"
	"./httpcommon"
	"./niconico/api"
	"./niconico/find"
	"./niconico/nicocas"
	"github.com/gin-gonic/gin"
)

type register struct {
	Target          string  `form:"target"`
	NicocasArchive  bool    `form:"nicocasArchive"`
	StartPositionEn bool    `form:"archive-position-en"`
	StartPosition   float64 `form:"startPositionSec"`
	ArchiveWait     float64 `form:"archive-wait"`
}

var defaultArchiveWait = float64(2)

type nicoFinder struct {
	Community []string `json:"community"`
	User      []string `json:"user"`
	Title     []string `json:"title"`
}

var nicoFinderFile = "nico-finder-setting.txt"

func loadNicoFinder() (data nicoFinder) {
	_, err := os.Stat(nicoFinderFile)
	if err == nil {
		f, err := os.Open(nicoFinderFile)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer f.Close()

		bs, err := ioutil.ReadAll(f)
		if err != nil {
			fmt.Println(err)
			return
		}

		err = json.Unmarshal(bs, &data)
		if err != nil {
			fmt.Println(err)
			return
		}

	}

	return
}

var limit = "2019/01/20 23:59:59 JST"

const layout = "2006/01/02 15:04:05 MST"

var actionTrackIDFile = "action-track-id.txt"
var actionTrackID string

var userSessionFile = "user-session.txt"
var userSession string

func main() {

	limitTime, err := time.Parse(layout, limit)
	if err != nil {
		fmt.Println(err)
		return
	}

	now := time.Now()
	if limitTime.Unix() < now.Unix() {
		fmt.Println("kigengire")
		return
	}

	ctx := context.Background()

	// userSession
	func() {
		f, err := os.Open(userSessionFile)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer f.Close()

		fmt.Fscanln(f, &userSession)
	}()
	if userSession == "" {
		fmt.Printf("%sにuser-session情報を保存して下さい", userSessionFile)
		return
	}

	// nico-actiontrackid
	func() {
		f, err := os.Open(actionTrackIDFile)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Fscanln(f, &actionTrackID)
		defer f.Close()

		if actionTrackID == "" {
			actionTrackID, err = api.GetActionTrackID(ctx)
			if err != nil {
				fmt.Println(err)
				return
			}

			f2, err := os.Create(actionTrackIDFile)
			if err != nil {
				fmt.Println(err)
				return
			}
			defer f2.Close()
			f2.WriteString(actionTrackID)
		}
	}()
	if actionTrackID == "" {
		fmt.Printf("%sにactionTrackID情報を保存して下さい", actionTrackIDFile)
		return
	}

	finder := find.NewFinder(ctx)
	// TODO：デフォルトの監視リストを設定から読み込む
	data := loadNicoFinder()
	if data.Community != nil {
		finder.SetCommunityList(data.Community)
	}
	if data.User != nil {
		finder.SetUserList(data.User)
	}
	if data.Title != nil {
		finder.SetTitleList(data.Title)
	}
	finder.Launch()

	go func() {
		for {
			select {
			case pid := <-finder.Found:

				fmt.Printf("found %v\n", pid)

				reg := register{
					Target:          fmt.Sprintf("lv%d", pid),
					NicocasArchive:  true,
					StartPositionEn: true,
					StartPosition:   0,
					ArchiveWait:     defaultArchiveWait,
				}
				registerTarget(ctx, reg)

			case <-finder.Closed:
				fmt.Println("[FIXME] 監視が終了しました")
				return
			}
		}
	}()

	convertQueueMap := sync.Map{}
	convertQueue := make(chan string, 256)
	go func() {
		for {
			path := <-convertQueue
			convertQueueMap.Store(path, true)
			convertTarget(ctx, path)
			convertQueueMap.Delete(path)
		}
	}()

	r := gin.New()
	r.LoadHTMLGlob("view/*")
	r.GET("/", func(c *gin.Context) {

		var nicoFinderWorking bool
		select {
		case <-finder.Closed:
		default:
			nicoFinderWorking = true
		}

		converts := map[string]bool{}
		convertQueueMap.Range(func(key, value interface{}) bool {
			converts[key.(string)] = value.(bool)
			return true
		})

		c.HTML(http.StatusOK, "index.html", gin.H{
			"title":   "livedl2 α0.1",
			"limit":   limitTime.Format(layout),
			"workers": works,
			"working": len(works) > 0,

			// ニコ生監視
			"NicoFinderWorking": nicoFinderWorking,
			"NicoCommunityList": finder.GetCommunityList(),
			"NicoUserList":      finder.GetUserList(),
			"NicoTitleList":     finder.GetTitleList(),
			// 変換リスト
			"converts": converts,
		})
	})
	r.POST("/register", func(c *gin.Context) {
		var reg register
		if e := c.ShouldBind(&reg); e != nil {
			fmt.Printf("%+v\n", reg)
		} else {
			if strings.HasPrefix(reg.Target, `"`) && strings.HasSuffix(reg.Target, `"`) {
				reg.Target = strings.TrimPrefix(reg.Target, `"`)
				reg.Target = strings.TrimSuffix(reg.Target, `"`)
			}

			if strings.HasSuffix(reg.Target, ".sqlite3") ||
				strings.HasSuffix(reg.Target, ".sqlite") || strings.HasSuffix(reg.Target, ".db") {

				convertQueue <- reg.Target
				convertQueueMap.Store(reg.Target, false)
				//convertTarget(ctx, reg)
			} else {
				registerTarget(ctx, reg)
			}
		}
		c.Redirect(http.StatusSeeOther, "/")
	})
	r.POST("/close", func(c *gin.Context) {
		var reg register
		fmt.Println("closing")
		if e := c.ShouldBind(&reg); e != nil {
			fmt.Printf("%+v\n", reg)
		} else {
			fmt.Printf("%#v\n", reg)
			if reg.Target == "all" {
				closeTargetAll()
			} else {
				closeTarget(reg.Target)
			}
		}
		c.Redirect(http.StatusSeeOther, "/")
	})
	r.GET("/get-list", func(c *gin.Context) {
		type work struct {
			WorkerID string `json:"workerId"`
			ID       string `json:"id"`
			Title    string `json:"title"`
			Name     string `json:"name"`
			Progress string `json:"progress"`
		}

		list := make([]work, 0, len(works))

		for k, v := range works {
			if k == "" {
				continue
			}

			w := work{
				WorkerID: k,
				ID:       v.GetID(),
				Title:    v.GetTitle(),
				Name:     v.GetName(),
				Progress: v.GetProgress(),
			}
			list = append(list, w)
		}

		c.JSON(200, gin.H{
			"result": list,
		})
	})

	// ニコ生監視設定
	r.POST("/nico-finder", func(c *gin.Context) {

		defer c.Request.Body.Close()
		bs, _ := ioutil.ReadAll(c.Request.Body)

		u, _ := url.Parse("?" + string(bs))
		v := u.Query()

		data := nicoFinder{
			Community: []string{},
			User:      []string{},
			Title:     []string{},
		}
		for k, v := range v {
			arr := make([]string, 0)
			for _, v := range v {
				if v != "" {
					arr = append(arr, v)
				}
			}
			switch k {
			case "community[]":
				finder.SetCommunityList(arr)
				data.Community = arr
			case "user[]":
				finder.SetUserList(arr)
				data.User = arr
			case "title[]":
				finder.SetTitleList(arr)
				data.Title = arr
			}
		}

		bs, err := json.MarshalIndent(data, "", "\t")
		if err != nil {
			fmt.Println(err)
		} else {
			f, err := os.Create(nicoFinderFile)
			if err != nil {
				fmt.Println(err)
			} else {
				_, err = f.Write(bs)
				if err != nil {
					fmt.Println(err)
				}
				f.Close()
			}
		}

		c.Redirect(http.StatusSeeOther, "/")
	})

	r.GET("/pprof/:name", func(c *gin.Context) {
		c.Request.Form = url.Values{}
		c.Request.Form.Set("debug", "1")
		name := c.Param("name")
		pprof.Handler(name).ServeHTTP(c.Writer, c.Request)
	})
	r.Run() // listen and serve on 0.0.0.0:8080
}

type downloadWork interface {
	Close()
	GetWorkerID() string
	GetID() string
	GetTitle() string
	GetName() string
	GetProgress() string
}

var works map[string]downloadWork

var closed chan string

var q chan httpcommon.HttpWork

func init() {
	works = make(map[string]downloadWork)
	closed = make(chan string, 10)

	q = httpcommon.Launch(10)

	go func() {
		for id := range closed {
			fmt.Printf("got close notify: %v\n", id)
			wrk, ok := works[id]
			if ok {
				closeWork(wrk)
				fmt.Printf("done close notify: %v\n", id)
				delete(works, id)
			}
		}
	}()
}

func closeWork(w downloadWork) {
	w.Close()
}

func closeTarget(target string) {
	wrk, ok := works[target]
	if ok {
		closeWork(wrk)
	}
}

func closeTargetAll() {
	for _, val := range works {
		closeWork(val)
	}
}

func registerTarget(ctx context.Context, reg register) {

	fmt.Printf("%#v\n", reg)

	var re *regexp.Regexp
	// 新配信
	/*
		re = regexp.MustCompile(`live2\.nicovideo\.jp/.+?/(lv\d+)`)
		if m := re.FindStringSubmatch(reg.Target); len(m) > 1 {
			fmt.Printf("%s\n", m[1])
			wrk = nicocas.ThisIsTest(ctx, m[1], 0)
			fmt.Printf("%#v\n", wrk)
			//cancelWork(wrk)
			return
		}
		// Nicocas
		re = regexp.MustCompile(`cas\.nicovideo\.jp/.+?/(lv\d+)`)
		if m := re.FindStringSubmatch(reg.Target); len(m) > 1 {
			fmt.Printf("cas %s\n", m[1])
			wrk = nicocas.ThisIsTest(ctx, m[1], 0)
			fmt.Printf("%#v\n", wrk)
			return
		}
	*/

	// youtube

	// twitcas

	// ニコ生

	re = regexp.MustCompile(`(lv\d+)`)
	if m := re.FindStringSubmatch(reg.Target); len(m) > 1 {
		id := m[1]
		fmt.Printf("%s\n", id)

		props, err := nicocas.GetProps(ctx, id, userSession)
		if err != nil {
			fmt.Println(err)
			return
		}

		workerID := nicocas.GetWorkerID(id)
		if _, ok := works[workerID]; !ok {

			fmt.Printf("%#v\n", props)

			wrk := nicocas.Create(ctx, q, closed, props, reg.NicocasArchive, reg.StartPositionEn, reg.StartPosition, reg.ArchiveWait, userSession, actionTrackID)

			works[workerID] = wrk
		} else {
			fmt.Printf("already started %v\n", id)
		}

		return
	}
}

func convertTarget(ctx context.Context, path string) {

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return
	}
	defer db.Close()

	mediaName := files.ChangeExtention(path, "ts")

	rows, err := db.QueryContext(ctx, `SELECT seqno, bandwidth, data FROM media ORDER BY seqno`)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer rows.Close()

	var seqno int64
	var bandwidth int64
	var data []byte

	prevSeqno := int64(-2) // 1足してもマイナスのseqNoになるようにする
	prevBandwidth := int64(-1)

	var f *os.File
	for rows.Next() {
		rows.Scan(&seqno, &bandwidth, &data)
		if prevSeqno+1 != seqno || prevBandwidth != bandwidth {

			fmt.Printf("prevSeqno%v != seqno%v, prevBandwidth%v != bandwidth%v\n",
				prevSeqno, seqno, prevBandwidth, bandwidth)

			if f != nil {
				f.Close()
			}
			nextFile, _ := files.GetFileNameNext(mediaName)
			f, err = os.Create(nextFile)
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		prevSeqno = seqno
		prevBandwidth = bandwidth

		_, err := f.Write(data)
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	if f != nil {
		f.Close()
	}

	fmt.Printf("変換終了: %s\n", mediaName)
}

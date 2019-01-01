package find

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"../../httpcommon"
)

var keepSecond = int64(300)

type doc struct {
	CategoryTags  string `json:"category_tags"`  // "一般(その他)"
	ChannelOnly   string `json:"channel_only"`   // "0"
	Community     string `json:"community"`      // coXXXX
	CommunityOnly string `json:"community_only"` // "0" コミュ限なら "1"
	Communityname string `json:"communityname"`  // コミュ名
	Description   string `json:"description"`    // 説明文
	ID            int64  `json:"id"`             // 放送ID(lvなし Number)
	Ownername     string `json:"ownername"`      // 放送者名
	ProviderClass string `json:"provider_class"` // "community"
	ProviderType  string `json:"provider_type"`  // "community"
	StartTime     int64  `json:"start_time"`     // 1546101768
	StreamStatus  string `json:"stream_status"`  // "onair"
	Title         string `json:"title"`          // Title
	User          string `json:"user"`           // UserId(string)
}

type docList map[int64]doc

type docs struct {
	Docs []doc `json:"docs"`
	//OnairDocs []doc `json:"onair_docs"`
}

var keywords = []string{
	"一般",
	"ゲーム",
	"やってみた",
	"動画紹介",
}

type Finder struct {
	_communityList []string
	_userList      []string
	_titleList     []string
	mtx            sync.Mutex
	list           docList
	ctx            context.Context
	cancelFunc     func()
	Closed         chan struct{} // 監視ループの終了
	Found          chan int64    // 見つかった番組ID
}

func NewFinder(ctx context.Context) *Finder {
	ctx2, cancelFunc := context.WithCancel(ctx)
	f := &Finder{
		ctx:        ctx2,
		cancelFunc: cancelFunc,
		Closed:     make(chan struct{}),
		Found:      make(chan int64, 100),
	}
	f.list = make(docList)
	return f
}
func (f *Finder) Close() {
	f.cancelFunc()
	select {
	case <-f.Closed:
	default:
		close(f.Closed)
	}
}

func (f *Finder) GetCommunityList() []string {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f._communityList
}

func (f *Finder) SetCommunityList(l []string) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f._communityList = l
}

func (f *Finder) GetUserList() []string {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f._userList
}

func (f *Finder) SetUserList(l []string) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f._userList = l
}

func (f *Finder) GetTitleList() []string {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f._titleList
}

func (f *Finder) SetTitleList(l []string) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f._titleList = l
}

func (f *Finder) filter(d doc, communityList, userList, titleList []string) bool {

	// community
	for _, c := range communityList {
		if d.Community == c {
			return true
		}
	}

	// user
	for _, c := range userList {
		if d.User == c {
			return true
		}
	}

	// title
	for _, t := range titleList {
		if strings.Contains(d.Title, t) {
			return true
		}
	}

	return false
}

func (f *Finder) Launch() {
	go f.run()
}

// メインループ
func (f *Finder) run() {
	for {

		// community
		communityList := f.GetCommunityList()
		// user
		userList := f.GetUserList()
		// title
		titleList := f.GetTitleList()

		if len(communityList) > 0 || len(userList) > 0 || len(titleList) > 0 {

			err := f.forKeywords(communityList, userList, titleList)
			if err != nil {
				fmt.Println(err)
			}

			// 一定時間以上経ったリストは削除
			now := time.Now().Unix()
			t := now - keepSecond

			for k, v := range f.list {
				if t > v.StartTime {
					delete(f.list, k)
				}
			}

		}

		select {
		case <-time.After(30 * time.Second):
		case <-f.Closed:
			return
		}
	}
}

func (f *Finder) forKeywords(communityList, userList, titleList []string) error {
	for _, k := range keywords {
		err := f.find(k, communityList, userList, titleList)
		if err != nil {
			return err
		}
		select {
		case <-time.After(1 * time.Second):
		case <-f.Closed:
			return nil
		}
	}
	return nil
}

func (f *Finder) find(keyword string, communityList, userList, titleList []string) (err error) {

	query := url.Values{}

	query.Set("track", "")
	query.Set("sort", "recent")
	query.Set("date", "")
	query.Set("keyword", keyword)
	query.Set("filter", " :nocommunitygroup:")
	query.Set("kind", "tags")
	query.Set("page", "1")

	uri := fmt.Sprintf("https://live.nicovideo.jp/search?%s", query.Encode())

	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return
	}

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

	now := time.Now().Unix()
	if ma := regexp.MustCompile(`Nicolive_JS_Conf\.Search\s*=\s*({.+?})\s*;`).FindSubmatch(dat); len(ma) > 0 {

		var docs docs
		if err = json.Unmarshal([]byte(string(ma[1])), &docs); err != nil {
			return
		}
		//objs.PrintAsJson(docs)

		for _, d := range docs.Docs {
			// まだ見つかってなかったもの
			if _, ok := f.list[d.ID]; !ok {
				f.list[d.ID] = d
				start := d.StartTime
				diff := now - start

				if diff < keepSecond && f.filter(d, communityList, userList, titleList) {
					fmt.Printf("Found lv%d %s\n", d.ID, d.Title)
					select {
					case f.Found <- d.ID:
					default:
						fmt.Printf("登録できませんでした: lv%d", d.ID)
					}
				}
			}
		}

	} else {
		//fmt.Println(string(dat))
	}

	return
}

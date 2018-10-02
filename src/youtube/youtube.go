package youtube

import (
	"fmt"
//	"net/http"
//	"io/ioutil"
	"bufio"
	"regexp"
	"encoding/json"
	"html"
	"context"
	"os"
	"os/signal"
	"syscall"
	"../objs"
	"../files"
	"../procs/streamlink"
	"../gorman"
	"../httpbase"

	"strings"
)

var Cookie = "PREF=f1=50000000&f4=4000000&hl=en"
var UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36"

func getChatContinuation(buff []byte) (isReplay bool, continuation string, err error) {

	if ma := regexp.MustCompile(`(?s)\Wwindow\["ytInitialData"\]\p{Zs}*=\p{Zs}*({.*?})\p{Zs}*;`).FindSubmatch(buff); len(ma) > 1 {
		var data interface{}
		err = json.Unmarshal(ma[1], &data)
		if err != nil {
			err = fmt.Errorf("ytInitialData parse error")
			return
		}

		//objs.PrintAsJson(data);

		liveChatRenderer, ok := objs.Find(data,
			"contents",
			"twoColumnWatchNextResults",
			"conversationBar",
			"liveChatRenderer",
		)
		if (! ok) {
			err = fmt.Errorf("liveChatRenderer not found")
			return
		}
		isReplay, _ = objs.FindBool(liveChatRenderer, "isReplay")

		subMenuItems, ok := objs.FindArray(liveChatRenderer,
			"header",
			"liveChatHeaderRenderer",
			"viewSelector",
			"sortFilterSubMenuRenderer",
			"subMenuItems",
		)
		if (! ok) {
			err = fmt.Errorf("subMenuItems not found")
			return
		}

		for _, item := range subMenuItems {
			title, _ := objs.FindString(item, "title")
			//selected, _ := objs.FindBool(item, "selected")
			c, _ := objs.FindString(item, "continuation", "reloadContinuationData", "continuation")

			if (title != "") && (! strings.Contains(title, "Top")) {
				continuation = c
				return
			}
			continuation = c
		}

	} else {
		err = fmt.Errorf("ytInitialData not found")
		return
	}

	if continuation == "" {
		err = fmt.Errorf("continuation not found")
	}
	return
}

func getInfo(buff []byte) (title, ucid, author string, err error) {
	var data interface{}
	re := regexp.MustCompile(`(?s)\Wytplayer\.config\p{Zs}*=\p{Zs}*({.*?})\p{Zs}*;`)
	if ma := re.FindSubmatch(buff); len(ma) > 1 {
		str := html.UnescapeString(string(ma[1]))
		if err = json.Unmarshal([]byte(str), &data); err != nil {
			err = fmt.Errorf("ytplayer parse error")
			return
		}
	} else {
		err = fmt.Errorf("ytplayer.config not found")
		return
	}

	//objs.PrintAsJson(data); return

	title, ok := objs.FindString(data, "args", "title")
	if (! ok) {
		err = fmt.Errorf("title not found")
		return
	}
	ucid, _ = objs.FindString(data, "args", "ucid")
	author, _ = objs.FindString(data, "args", "author")
	return
}

func Record(id string) (err error) {

	uri := fmt.Sprintf("https://www.youtube.com/watch?v=%s", id)
	code, buff, err, neterr := httpbase.GetBytes(uri, map[string]string {
		"Cookie": Cookie,
		"User-Agent": UserAgent,
	})
	if err != nil {
		return
	}
	if neterr != nil {
		return
	}
	if code != 200 {
		neterr = fmt.Errorf("Status code: %v\n", code)
		return
	}

	title, ucid, author, err := getInfo(buff)
	if err != nil {
		return
	}

	if false {
		fmt.Println(ucid)
	}

	isReplay, continuation, err := getChatContinuation(buff)


	origName := fmt.Sprintf("%s-%s_%s.mp4", author, title, id)
	origName = files.ReplaceForbidden(origName)
	name, err := files.GetFileNameNext(origName)
	if err != nil {
		fmt.Println(err)
		return
	}

fmt.Println(name)
	cmd, stderr, err := streamlink.Open(uri, "best", "--retry-max", "10", "-o", name)
	if err != nil {
		fmt.Println(err)
		return
	}

	var gm *gorman.GoroutineManager
	gm = gorman.WithChecker(func(c int) {
		if c != 0 {
			gm.Cancel()
		}
	})

	chInterrupt := make(chan os.Signal, 10)
	signal.Notify(chInterrupt, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(chInterrupt)

	ctx, cancel := context.WithCancel(context.Background())

	gm.Go(func(c <-chan struct{}) int {
		select {
		case <-chInterrupt:
		case <-c:
		}

		cancel()
		gm.Cancel()
		return 1
	})

	gm.Go(func(c <-chan struct{}) int {
		defer stderr.Close()
		rdr := bufio.NewReader(stderr)
		re := regexp.MustCompile(`(Written.*?)\s*\z`)
		for {
			select {
			case <-c: return 0
			default:
			}

			s, err := rdr.ReadString('\r')
			if err != nil {
				return 0
			}
			if ma := re.FindStringSubmatch(s); len(ma) > 1 {
				fmt.Printf("%s: %s\n", name, ma[1])
			}
		}
		return 0
	})

	if continuation != "" {
		gm.Go(func(c <-chan struct{}) int {
			getComment(gm, ctx, c, isReplay, continuation, origName)
			return 0
		})
	}

	gm.Go(func(c <-chan struct{}) int {
		select {
		case <-c: cancel()
		}
		return 0
	})


	cmd.Wait()

	gm.Cancel()
	gm.Wait()

	return
}

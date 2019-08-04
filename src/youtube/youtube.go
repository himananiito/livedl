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
	"../procs/youtube_dl"
	"../gorman"
	"../httpbase"

	"strings"
	"time"
	"sync"
	"../procs"
)

var Cookie = "PREF=f1=50000000&f4=4000000&hl=en"
var UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36"

var split = func(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i := 0; i < len(data) ; i++ {
		if data[i] == '\n' {
			return i + 1, data[:i + 1], nil
		}
		if data[i] == '\r' {
			if (i + 1) == len(data) {
				return 0, nil, nil
			}
			if data[i + 1] == '\n' {
				return i + 2, data[:i + 2], nil
			}
			return i + 1, data[:i + 1], nil
		}
	}

	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}

	return 0, nil, nil
}

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

	var player_response interface{}
	err = json.Unmarshal([]byte(data.(map[string]interface{})["args"].(map[string]interface{})["player_response"].(string)), &player_response)
	if err != nil {
		err = fmt.Errorf("player_response parse error")
		return
	}

	//objs.PrintAsJson(player_response); return

	title, ok := objs.FindString(player_response, "videoDetails", "title")
	if (! ok) {
		err = fmt.Errorf("title not found")
		return
	}
	ucid, _ = objs.FindString(data, "args", "ucid")
	author, _ = objs.FindString(player_response, "videoDetails", "author")
	return
}

func execStreamlink(gm *gorman.GoroutineManager, uri, name string) (notSupport bool, err error) {
	cmd, stdout, stderr, err := streamlink.Open(uri, "best", "--retry-max", "10", "-o", name)
	if err != nil {
		return
	}
	defer stdout.Close()
	defer stderr.Close()

	chStdout := make(chan string, 10)
	chStderr := make(chan string, 10)
	chEof := make(chan struct{}, 2)

	// stdout
	gm.Go(func(c <-chan struct{}) int {
		defer func(){
			chEof <- struct{}{}
		}()
		scanner := bufio.NewScanner(stdout)
		scanner.Split(split)

		for scanner.Scan() {
			chStdout <- scanner.Text()
		}

		return 0
	})

	// stderr
	gm.Go(func(c <-chan struct{}) int {
		defer func(){
			chEof <- struct{}{}
		}()
		scanner := bufio.NewScanner(stderr)
		scanner.Split(split)

		for scanner.Scan() {
			chStderr <- scanner.Text()
		}

		return 0
	})


	// outputs
	gm.Go(func(c <-chan struct{}) int {
		for {
			var s string
			select {
			case s = <-chStdout:
			case s = <-chStderr:
			case <-chEof:
				return 0
			}

			if strings.HasPrefix(s, "[cli][error]") {
				fmt.Print(s)

				notSupport = true
				procs.Kill(cmd.Process.Pid)
				break
			} else if strings.HasPrefix(s, "Traceback (most recent call last):") {
				fmt.Print(s)

				notSupport = true
				//procs.Kill(cmd.Process.Pid)
				//break
			} else {
				fmt.Print(s)
			}
		}
		return 0
	})

	cmd.Wait()

	return
}

func execYoutube_dl(gm *gorman.GoroutineManager, uri, name string) (err error) {
	defer func() {
		part := name + ".part"
		if _, test := os.Stat(part); test == nil {
			if _, test := os.Stat(name); test != nil {
				os.Rename(part, name)
			}
		}
	}()

	cmd, stdout, stderr, err := youtube_dl.Open("--no-mtime", "--no-color", "-o", name, uri)
	if err != nil {
		return
	}
	defer stdout.Close()
	defer stderr.Close()

	chStdout := make(chan string, 10)
	chStderr := make(chan string, 10)
	chEof := make(chan struct{}, 2)

	// stdout
	gm.Go(func(c <-chan struct{}) int {
		defer func(){
			chEof <- struct{}{}
		}()
		scanner := bufio.NewScanner(stdout)
		scanner.Split(split)

		for scanner.Scan() {
			chStdout <- scanner.Text()
		}

		return 0
	})

	// stderr
	gm.Go(func(c <-chan struct{}) int {
		defer func(){
			chEof <- struct{}{}
		}()
		scanner := bufio.NewScanner(stderr)
		scanner.Split(split)

		for scanner.Scan() {
			chStderr <- scanner.Text()
		}

		return 0
	})

	// outputs
	gm.Go(func(c <-chan struct{}) int {
		var old int64
		for {
			var s string
			select {
			case s = <-chStdout:
			case s = <-chStderr:
			case <-chEof:
				return 0
			}

			if strings.HasPrefix(s, "[https @ ") {
				// ffmpeg unwanted logs
			} else {
				if strings.HasPrefix(s, "[download]") {
					var now = time.Now().UnixNano()
					if now - old > 2 * 1000 * 1000 * 1000 {
						old = now
					} else {
						continue
					}
				}
				fmt.Print(s)
			}
		}
		return 0
	})

	cmd.Wait()
	return
}

var COMMENT_DONE = 1000

func Record(id string, ytNoStreamlink, ytNoYoutube_dl bool) (err error) {

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

	mtxComDone := &sync.Mutex{}
	var commentDone bool

	var gm *gorman.GoroutineManager
	var gmCom *gorman.GoroutineManager

	gm = gorman.WithChecker(func(c int) {
		switch c {
		case 0:
		default:
			gm.Cancel()
			if gmCom != nil {
				gmCom.Cancel();
			}
		}
	})

	gmCom = gorman.WithChecker(func(c int) {
		switch c {
		case 0:
		case COMMENT_DONE:
			func() {
				mtxComDone.Lock()
				defer mtxComDone.Unlock()
				commentDone = true;
			}()
		default:
			gmCom.Cancel()
		}
	})

	chInterrupt := make(chan os.Signal, 10)
	signal.Notify(chInterrupt, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(chInterrupt)

	ctx, cancel := context.WithCancel(context.Background())

	var interrupt bool
	gm.Go(func(c <-chan struct{}) int {
		select {
		case <-chInterrupt:
			interrupt = true
		case <-c:
		}

		cancel()
		gm.Cancel()
		return 1
	})

	if continuation != "" {
		gmCom.Go(func(c <-chan struct{}) int {
			getComment(gmCom, ctx, c, isReplay, continuation, origName)
			fmt.Printf("\ncomment done\n")
			return COMMENT_DONE
		})
	}

	gm.Go(func(c <-chan struct{}) int {
		select {
		case <-c: cancel()
		}
		return 0
	})

	var retry bool
	if (! ytNoStreamlink) {
		retry, err = execStreamlink(gm, uri, name)
	}
	if !interrupt {
		if err != nil || retry || (ytNoStreamlink && (! ytNoYoutube_dl)) {
			execYoutube_dl(gm, uri, name)
		}
	}

	if continuation != "" {
		if isReplay {
			if !commentDone {
				fmt.Printf("\nwaiting comment\n")
				gmCom.Wait()
			} else {
				gmCom.Wait()
			}

		} else {
			gmCom.Cancel()
			gmCom.Wait()
		}
	}

	gm.Cancel()
	gm.Wait()

	return
}

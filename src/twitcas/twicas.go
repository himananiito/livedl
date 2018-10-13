package twitcas

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/gorilla/websocket"
	"../files"
	"../httpbase"
	"../procs/ffmpeg"
	"os/exec"
	"io"
)

type Twitcas struct {
	Conn *websocket.Conn
}

func connectStream(proto, host, mode string, id uint64, proxy string) (conn *websocket.Conn, err error) {
	streamUrl := fmt.Sprintf(
		//case A.InnerFrame:return"i";
		//case A.Pframe:return"p";
		//case A.DisposableProfile:return"bd";
		//case A.Bframe:return"b";
		//case A.Any:return"any";
		//case A.KeyFrame:default:return"k"}
		//"%s://%s/ws.app/stream/%d/fmp4/k/0/1?mode=%s",
		"%s://%s/ws.app/stream/%d/fmp4/bd/1/1500?mode=%s",
		proto, host, id, mode,
	)
	// fmt.Println(streamUrl)

	var origin string
	if proto == "wss" {
		origin = fmt.Sprintf("https://%s", host)
	} else {
		origin = fmt.Sprintf("http://%s", host)
	}

	header := http.Header{}
	header.Set("Origin", origin)
	//header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.132 Safari/537.36")
	header.Set("User-Agent", httpbase.GetUserAgent())

	timeout, _ := time.ParseDuration("10s")
	dialer := websocket.Dialer{
		HandshakeTimeout: timeout,
	}
	if proxy != "" {
		dialer.Proxy = func(req *http.Request) (u *url.URL, err error) {
			var proxyUrl string
			if proto == "wss" {
				proxyUrl = fmt.Sprintf("https://%s", proxy)
			} else {
				proxyUrl = fmt.Sprintf("http://%s", proxy)
			}
			return url.ParseRequestURI(proxyUrl)
		}
	}

	conn, _, err = dialer.Dial(streamUrl, header)

	return
}

func getStream(user, proxy string) (conn *websocket.Conn, movieId uint64, err error) {
	url := fmt.Sprintf(
		"https://twitcasting.tv/streamserver.php?target=%s&mode=client",
		user,
	)

	type StreamServer struct {
		Movie struct {
			Id   uint64 `json:"id"`
			Live bool   `json:"live"`
		} `json:"movie"`
		Fmp4 struct {
			Host   string `json:"host"`
			Proto  string `json:"proto"`
			Source bool   `json:"source"`
			MobileSource bool `json:"mobilesource"`
		} `json:"fmp4"`
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}

	client := new(http.Client)
	client.Timeout, _ = time.ParseDuration("10s")

	resp, err := client.Do(req)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	//fmt.Printf("debug %s\n", string(respBytes))

	data := new(StreamServer)

	err = json.Unmarshal(respBytes, data)
	if err != nil {
		return
	}

	if !data.Movie.Live {
		// movie not active
		err = errors.New(user + " --> " + "Offline or User Not Found")
		return
	} else {
		var mode string
		if data.Fmp4.Source {
			// StreamQuality.High
			mode = "main"
		} else if data.Fmp4.MobileSource {
			// StreamQuality.Middle
			mode = "mobilesource"
		} else {
			// StreamQuality.Low
			mode = "base"
		}

		if data.Fmp4.Proto != "" && data.Fmp4.Host != "" && data.Movie.Id != 0 {
			conn, err = connectStream(data.Fmp4.Proto, data.Fmp4.Host, mode, data.Movie.Id, proxy)
			if err != nil {
				return
			}
			movieId = data.Movie.Id
		} else {
			err = errors.New(user + " --> " + "No Stream Defined")
			return
		}
	}

	return
}

func createFileUser(user string, movieId uint64) (f *os.File, filename string, err error) {
	user = files.ReplaceForbidden(user)
	filename = fmt.Sprintf("%s_%d.mp4", user, movieId)
	for i := 2; i < 1000; i++ {
		_, err := os.Stat(filename)
		if err != nil {
			break
		}
		filename = fmt.Sprintf("%s_%d_%d.mp4", user, movieId, i)
	}
	f, err = os.Create(filename)
	return
}

// FIXME: return codeの整理
func TwitcasRecord(user, proxy string) (done, dbLocked bool) {
	conn, movieId, err := getStream(user, proxy)
	if err != nil {
		fmt.Printf("@err getStream: %v\n", err)
		return
	}
	if conn == nil {
		fmt.Println("[FIXME] conn is nil")
		return
	}
	defer conn.Close()

	dbName := fmt.Sprintf("tmp/tcas-%v-lock.db", movieId)
	files.MkdirByFileName(dbName)
	db, err := sql.Open("sqlite3", dbName)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	_, err = db.Exec(`BEGIN EXCLUSIVE`)
	if err != nil {
		dbLocked = true
		return
	}
	defer os.Remove(dbName)

	//func Open(opt... string) (cmd *exec.Cmd, stdin io.WriteCloser, err error) {
	var cmd *exec.Cmd
	var stdin io.WriteCloser

	var fileOpened bool

	filenameBase := fmt.Sprintf("%s_%d.ts", user, movieId)
	filenameBase = files.ReplaceForbidden(filenameBase) // fixed #8

	closeFF := func() {
		if stdin != nil {
			stdin.Close()
		}
		if cmd != nil {
			cmd.Wait()
		}
		stdin = nil
		cmd = nil
	}

	openFF := func() (err error) {
		closeFF()

		filename, err := files.GetFileNameNext(filenameBase)
		if err != nil {
			fmt.Println(err)
			return
		}

		c, in, err := ffmpeg.Open("-i", "-", "-c", "copy", "-y", filename)
		if err != nil {
			return
		}
		cmd = c
		stdin = in

		fileOpened = true
		return
	}

	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("@err ReadMessage: %v\n\n", err)
			return
		}

		if messageType == 2 {
			if cmd == nil || stdin == nil {
				if err = openFF(); err != nil {
					fmt.Println(err)
					return
				}
				defer closeFF()
			}

			if _, err := stdin.Write(data); err != nil {
				fmt.Println(err)
				return
			}

		} else if messageType == 1 {

			type TextMessage struct {
				Code int `json:"code"`
			}
			msg := new(TextMessage)
			err = json.Unmarshal(data, msg)
			if err != nil {
				// json decode error
				fmt.Printf("@err %v\n", err)
				return
			}
			if (msg.Code == 100) || (msg.Code == 101) || (msg.Code == 110) {
				// ignore
			} else if msg.Code == 400 { // invalid_parameter
				return
			} else if msg.Code == 401 { // passcode_required
				return
			} else if msg.Code == 403 { //access_forbidden
				return
			} else if msg.Code == 500 { // offline
				return
			} else if msg.Code == 503 { // server_error
				return
			} else if msg.Code == 504 { // live_ended
				break
			} else {
				fmt.Printf("@FIXME %v\n\n", string(data))
				return
			}
		}
	}

	closeFF()

	done = fileOpened
	return
}

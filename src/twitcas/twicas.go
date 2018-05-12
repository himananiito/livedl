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

	"github.com/gorilla/websocket"
)

type Twitcas struct {
	Conn *websocket.Conn
}

func connectStream(proto, host, mode string, id uint64, proxy string) (conn *websocket.Conn, err error) {
	streamUrl := fmt.Sprintf(
		"%s://%s/ws.app/stream/%d/fmp4/k/0/1?mode=%s",
		proto, host, id, mode,
	)

	var origin string
	if proto == "wss" {
		origin = fmt.Sprintf("https://%s", host)
	} else {
		origin = fmt.Sprintf("http://%s", host)
	}

	header := http.Header{}
	header.Set("Origin", origin)
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.132 Safari/537.36")

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

	client := new(http.Client)
	client.Timeout, _ = time.ParseDuration("10s")

	resp, e := client.Do(req)
	if e != nil {
		return
	}

	defer resp.Body.Close()
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	//respStr := string(respBytes)
	//fmt.Printf("@respStr %v\n", respStr)

	data := new(StreamServer)

	err = json.Unmarshal(respBytes, data)
	if err != nil {
		return
	}

	if !data.Movie.Live {
		// movie not active
		if data.Movie.Id == 0 {
			err = errors.New(user + " --> " + "User Not Found")
		} else {
			err = errors.New(user + " --> " + "Offline")
		}
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
			movieId = data.Movie.Id
		} else {
			err = errors.New(user + " --> " + "No Stream Defined")
			return
		}
	}

	return
}

func createFileUser(user string, movieId uint64) (file *os.File, filename string, err error) {
	filename = fmt.Sprintf("%s_%d.mp4", user, movieId)
	for i := 2; i < 1000; i++ {
		_, err := os.Stat(filename)
		if err != nil {
			break
		}
		filename = fmt.Sprintf("%s_%d_%d.mp4", user, movieId, i)
	}
	file, err = os.Create(filename)
	return
}

// FIXME: return codeの整理
func TwitcasRecord(user, proxy string) int {
	conn, movieId, err := getStream(user, proxy)
	if err != nil {
		fmt.Printf("@err %v\n", err)
		return 1
	}
	defer conn.Close()

	var file *os.File
	var fileOpened bool
	var filename string
	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("@err %v\n\n", err)
			return 9
		}

		if messageType == 2 {
			wlen, err := file.Write(data)
			if err != nil {
				if ! fileOpened {
					file, filename, err = createFileUser(user, movieId)
					if err != nil {
						fmt.Printf("@err %v\n\n", err)
						return 2
					}
					defer file.Close()
					fileOpened = true

					fmt.Printf("@Opend %s\n", filename)

					_, err = file.Write(data)
					if err != nil {
						fmt.Printf("@err %v\n", err)
						return 3
					}
				} else {
					// write error
					fmt.Printf("@err %v\n", err)
					return 8
				}
			}
			fmt.Printf("@Wrote %s --> %d bytes\n", filename, wlen)

		} else if messageType == 1 {

			type TextMessage struct {
				Code int `json:"code"`
			}
			msg := new(TextMessage)
			err = json.Unmarshal(data, msg)
			if err != nil {
				// json decode error
				fmt.Printf("@err %v\n", err)
				return 7
			}
			if (msg.Code == 100) || (msg.Code == 101) || (msg.Code == 110) {
				// ignore
			} else if msg.Code == 400 { // invalid_parameter
				return 400
			} else if msg.Code == 401 { // passcode_required
				return 401
			} else if msg.Code == 403 { //access_forbidden
				return 403
			} else if msg.Code == 500 { // offline
				return 500
			} else if msg.Code == 503 { // server_error
				return 503
			} else if msg.Code == 504 { // live_ended
				break
			} else {
				fmt.Printf("@FIXME %v\n\n", string(data))
				return 999
			}
		}
	}
	return 0
}

package youtube

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"html"
	"io/ioutil"

	"github.com/himananiito/livedl/files"
	"github.com/himananiito/livedl/gorman"
	"github.com/himananiito/livedl/httpbase"
	"github.com/himananiito/livedl/objs"
	_ "github.com/mattn/go-sqlite3"
)

type OBJ = map[string]interface{}

func getComment(gm *gorman.GoroutineManager, ctx context.Context, sig <-chan struct{}, isReplay bool, commentStart float64, continuation, name string) (done bool) {

	dbName := files.ChangeExtention(name, "yt.sqlite3")
	db, err := dbOpen(ctx, dbName)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	mtx := &sync.Mutex{}

	testContinuation, count, _ := dbGetContinuation(ctx, db, mtx)
	if commentStart < 0.5 && testContinuation != "" {
		continuation = testContinuation
	}

	var printTime int64
	var isFirst bool = true

MAINLOOP:
	for {
		select {
		case <-ctx.Done():
			break MAINLOOP
		case <-sig:
			break MAINLOOP
		default:
		}
		timeoutMs, _done, err, neterr := func() (timeoutMs int, _done bool, err, neterr error) {
			var uri string
			if isReplay {
				uri = fmt.Sprintf("https://www.youtube.com/youtubei/v1/live_chat/get_live_chat_replay?key=AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8")
			} else {
				uri = fmt.Sprintf("https://www.youtube.com/youtubei/v1/live_chat/get_live_chat?key=AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8")
			}

			postData := OBJ{
				"context": OBJ{
					"client": OBJ{
						"clientName": "WEB",
						"clientVersion": "2.20210128.02.00",
					},
				},
				"continuation": continuation,
			}
			if !isFirst && isReplay && commentStart > 1.5 {
				postData["currentPlayerState"] = OBJ{
					"playerOffsetMs": commentStart * 1000.0,
				}
			}
			resp, err, neterr := httpbase.PostJson(uri, map[string]string {
				"Cookie": Cookie,
				"User-Agent": UserAgent,
			}, postData)
			if err != nil {
				return
			}
			if neterr != nil {
				return
			}
			buff, neterr := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			if neterr != nil {
				return
			}
			code := resp.StatusCode
			if code != 200 {
				if code == 404 {
					fmt.Printf("Status code: %v (ignored)\n", code)
					time.Sleep(1000 * time.Millisecond)
					return
				} else {
					neterr = fmt.Errorf("Status code: %v\n", code)
					return
				}
			}

			var data interface{}
			err = json.Unmarshal(buff, &data)
			if err != nil {
				err = fmt.Errorf("json decode error")
				return
			}

			liveChatContinuation, ok := objs.Find(data, "continuationContents", "liveChatContinuation")
			if !ok {
				err = fmt.Errorf("(response liveChatContinuation) not found")
				return
			}

			if isFirst && isReplay && commentStart > 1.5 {
				isFirst = false
			} else if actions, ok := objs.FindArray(liveChatContinuation, "actions"); ok {
				var videoOffsetTimeMsec string

				for _, a := range actions {
					var item interface{}
					var ok bool
					item, ok = objs.Find(a, "addChatItemAction", "item")
					if !ok {
						item, ok = objs.Find(a, "addLiveChatTickerItemAction", "item")
						if !ok {
							item, ok = objs.Find(a, "replayChatItemAction", "actions", "addChatItemAction", "item")
							if ok {
								videoOffsetTimeMsec, _ = objs.FindString(a, "replayChatItemAction", "videoOffsetTimeMsec")
							}
						}
					}
					if !ok {
						//objs.PrintAsJson(a)
						//fmt.Println("(actions item) not found")
						continue
					}

					var liveChatMessageRenderer interface{}
					liveChatMessageRenderer, ok = objs.Find(item, "liveChatTextMessageRenderer")
					if !ok {
						liveChatMessageRenderer, ok = objs.Find(item, "liveChatPaidMessageRenderer")
					}
					if !ok {
						continue
					}

					authorExternalChannelId, _ := objs.FindString(liveChatMessageRenderer, "authorExternalChannelId")
					authorName, _ := objs.FindString(liveChatMessageRenderer, "authorName", "simpleText")
					id, ok := objs.FindString(liveChatMessageRenderer, "id")
					if !ok {
						continue
					}
					var message string
					if runs, ok := objs.FindArray(liveChatMessageRenderer, "message", "runs"); ok {
						for _, run := range runs {
							if text, ok := objs.FindString(run, "text"); ok {
								message += text
							} else if emojis, ok := objs.FindArray(run, "emoji", "shortcuts"); ok {
								if emoji, ok := emojis[0].(string); ok {
									message += emoji
								}
							}
						}
					}
					var others string
					var amount string
					amount, _ = objs.FindString(liveChatMessageRenderer, "purchaseAmountText", "simpleText")
					if amount != "" {
						others += ` amount="` + html.EscapeString(amount) +  `"`
					}
					timestampUsec, ok := objs.FindString(liveChatMessageRenderer, "timestampUsec")
					if !ok {
						continue
					}

					if false {
						fmt.Printf("%v ", videoOffsetTimeMsec)
						fmt.Printf("%v %v %v %v %v %v [%v ]\n", timestampUsec, count, authorName, authorExternalChannelId, message, id, others)
					}

					dbInsert(ctx, gm, db, mtx,
						id,
						timestampUsec,
						videoOffsetTimeMsec,
						authorName,
						authorExternalChannelId,
						message,
						continuation,
						others,
						count,
					)
					count++
				}

				// アーカイブ時、20秒毎に進捗を表示
				if videoOffsetTimeMsec != "" {
					now := time.Now().Unix()
					if now-printTime > 20 {
						printTime = now
						if msec, e := strconv.ParseInt(videoOffsetTimeMsec, 10, 64); e == nil {
							total := msec / 1000
							hour := total / 3600
							min := (total % 3600) / 60
							sec := (total % 3600) % 60
							fmt.Printf("comment pos: %02d:%02d:%02d\n", hour, min, sec)
						}
					}
				}

				//fmt.Println("------------")
			}

			if continuations, ok := objs.Find(liveChatContinuation, "continuations"); ok {
				//objs.PrintAsJson(continuations)

				if c, ok := objs.FindString(continuations, "timedContinuationData", "continuation"); ok {
					continuation = c
				} else if c, ok := objs.FindString(continuations, "liveChatReplayContinuationData", "continuation"); ok {
					continuation = c
				} else if c, ok := objs.FindString(continuations, "invalidationContinuationData", "continuation"); ok {
					continuation = c
				} else if c, ok := objs.FindString(continuations, "playerSeekContinuationData", "continuation"); ok {
					if isReplay {
						_done = true
						return
					}
					continuation = c
				} else {
					objs.PrintAsJson(continuations)
					err = fmt.Errorf("(liveChatContinuation continuation) not found")
					return
				}

				if t, ok := objs.FindString(continuations, "timedContinuationData", "timeoutMs"); ok {
					timeout, err := strconv.Atoi(t)
					if err != nil {
						timeoutMs = timeout
					}
				} else if t, ok := objs.FindString(continuations, "invalidationContinuationData", "continuation"); ok {
					timeout, err := strconv.Atoi(t)
					if err != nil {
						timeoutMs = timeout
					}
				}

			} else {
				objs.PrintAsJson(liveChatContinuation)
				err = fmt.Errorf("(liveChatContinuation>continuations) not found")
				return
			}

			return
		}()
		if err != nil {
			fmt.Println(err)
			break
		}
		if neterr != nil {
			fmt.Println(neterr)
			break
		}
		if _done {
			done = true
			break MAINLOOP
		}

		if timeoutMs < 1000 {
			if isReplay {
				timeoutMs = 1000
			} else {
				timeoutMs = 6000
			}
		}
		time.Sleep(time.Duration(timeoutMs) * time.Millisecond)
	}
	return
}

func dbOpen(ctx context.Context, name string) (db *sql.DB, err error) {
	db, err = sql.Open("sqlite3", name)
	if err != nil {
		return
	}

	_, err = db.ExecContext(ctx, `
		PRAGMA synchronous = OFF;
		PRAGMA journal_mode = WAL;
	`)
	if err != nil {
		db.Close()
		return
	}

	err = dbCreate(ctx, db)
	if err != nil {
		db.Close()
	}
	return
}

func dbCreate(ctx context.Context, db *sql.DB) (err error) {
	// table media

	_, err = db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS comment (
		id                  TEXT PRIMARY KEY NOT NULL UNIQUE,
		timestampUsec       INTEGER NOT NULL,
		videoOffsetTimeMsec INTEGER,
		authorName          TEXT,
		channelId           TEXT,
		message             TEXT,
		continuation        TEXT,
		others              TEXT,
		count               INTEGER NOT NULL
	)
	`)
	if err != nil {
		return
	}

	_, err = db.ExecContext(ctx, `
	CREATE UNIQUE INDEX IF NOT EXISTS comment0 ON comment(id);
	CREATE INDEX IF NOT EXISTS comment1 ON comment(timestampUsec);
	CREATE INDEX IF NOT EXISTS comment2 ON comment(videoOffsetTimeMsec);
	CREATE INDEX IF NOT EXISTS comment3 ON comment(count);
	`)
	if err != nil {
		return
	}

	return
}

func dbInsert(ctx context.Context, gm *gorman.GoroutineManager, db *sql.DB, mtx *sync.Mutex,
	id, timestampUsec, videoOffsetTimeMsec, authorName, authorExternalChannelId, message, continuation, others string, count int) {

	usec, err := strconv.ParseInt(timestampUsec, 10, 64)
	if err != nil {
		fmt.Printf("ParseInt error: %s\n", timestampUsec)
		return
	}
	var offset interface{}
	if videoOffsetTimeMsec == "" {
		offset = nil
	} else {
		n, err := strconv.ParseInt(videoOffsetTimeMsec, 10, 64)
		if err != nil {
			offset = nil
		} else {
			offset = n
		}
	}

	query := `INSERT OR IGNORE INTO comment
		(id, timestampUsec, videoOffsetTimeMsec, authorName, channelId, message, continuation, others, count) VALUES (?,?,?,?,?,?,?,?,?)`

	gm.Go(func(<-chan struct{}) int {
		mtx.Lock()
		defer mtx.Unlock()

		if _, err := db.ExecContext(ctx, query,
			id, usec, offset, authorName, authorExternalChannelId, message, continuation, others, count,
		); err != nil {
			if err.Error() != "context canceled" {
				fmt.Println(err)
			}
			return 1
		}
		return 0
	})

	return
}

func dbGetContinuation(ctx context.Context, db *sql.DB, mtx *sync.Mutex) (res string, cnt int, err error) {
	mtx.Lock()
	defer mtx.Unlock()

	err = db.QueryRowContext(ctx, "SELECT continuation, count FROM comment ORDER BY count DESC LIMIT 1").Scan(&res, &cnt)
	return
}

var SelComment = `SELECT
	timestampUsec,
	IFNULL(videoOffsetTimeMsec, -1),
	authorName,
	channelId,
	message,
	others,
	count
	FROM comment
	ORDER BY timestampUsec
`

func WriteComment(db *sql.DB, fileName string) {

	rows, err := db.Query(SelComment)
	if err != nil {
		log.Println(err)
		return
	}
	defer rows.Close()

	fileName = files.ChangeExtention(fileName, "xml")

	dir := filepath.Dir(fileName)
	base := filepath.Base(fileName)
	base, err = files.GetFileNameNext(base)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fileName = filepath.Join(dir, base)
	f, err := os.Create(fileName)
	if err != nil {
		log.Fatalln(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\r\n", `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintf(f, "%s\r\n", `<packet>`)

	firstOffsetUsec := int64(-1)

	for rows.Next() {
		var timestampUsec int64
		var videoOffsetTimeMsec int64
		var authorName string
		var channelId string
		var message string
		var others string
		var count int64

		err = rows.Scan(
			&timestampUsec,
			&videoOffsetTimeMsec,
			&authorName,
			&channelId,
			&message,
			&others,
			&count,
		)
		if err != nil {
			log.Println(err)
			return
		}

		var vpos int64
		if videoOffsetTimeMsec >= 0 {
			vpos = videoOffsetTimeMsec / 10
		} else {
			if firstOffsetUsec < 0 {
				firstOffsetUsec = timestampUsec
			}
			diff := timestampUsec - firstOffsetUsec
			vpos = diff / (10 * 1000)
		}

		line := fmt.Sprintf(
			`<chat vpos="%d" date="%d" date_usec="%d" user_id="%s" name="%s"%s no="%d"`,
			vpos,
			(timestampUsec / (1000 * 1000)),
			(timestampUsec % (1000 * 1000)),
			channelId,
			html.EscapeString(authorName),
			others,
			count,
		)

		line += ">"
		message = html.EscapeString(message)
		line += message
		line += "</chat>"
		fmt.Fprintf(f, "%s\r\n", line)
	}
	fmt.Fprintf(f, "%s\r\n", `</packet>`)
}

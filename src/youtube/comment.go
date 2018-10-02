package youtube

import (
	"fmt"
	"context"
	"time"
	"sync"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"strconv"
	"encoding/json"

	"../gorman"
	"../files"
	"../httpbase"
	"../objs"
)

func getComment(gm *gorman.GoroutineManager, ctx context.Context, sig <-chan struct{}, isReplay bool, continuation, name string) {

	dbName := files.ChangeExtention(name, "sqlite3")
	db, err := dbOpen(ctx, dbName)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	mtx := &sync.Mutex{}
	MAINLOOP: for {
		select {
		case <-ctx.Done(): break MAINLOOP
		case <-sig: break MAINLOOP
		default:
		}
		timeoutMs, err, neterr := func() (timeoutMs int, err, neterr error) {
			var uri string
			if isReplay {
				uri = fmt.Sprintf("https://www.youtube.com/live_chat_replay?continuation=%s&pbj=1", continuation)
			} else {
				uri = fmt.Sprintf("https://www.youtube.com/live_chat/get_live_chat?continuation=%s&pbj=1", continuation)
			}

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

			var data interface{}
			err = json.Unmarshal(buff, &data)
			if err != nil {
				err = fmt.Errorf("json decode error")
				return
			}

			liveChatContinuation, ok := objs.Find(data, "response", "continuationContents", "liveChatContinuation")
			if (! ok) {
				err = fmt.Errorf("(response liveChatContinuation) not found")
				return
			}

			if actions, ok := objs.FindArray(liveChatContinuation, "actions"); ok {
				for _, a := range actions {
					var item interface{}
					var ok bool
					var videoOffsetTimeMsec string
					item, ok = objs.Find(a, "addChatItemAction", "item")
					if (! ok) {
						item, ok = objs.Find(a, "addLiveChatTickerItemAction", "item")
						if (! ok) {
							item, ok = objs.Find(a, "replayChatItemAction", "actions", "addChatItemAction", "item")
							if ok {
								videoOffsetTimeMsec, _ = objs.FindString(a, "replayChatItemAction", "videoOffsetTimeMsec")
							}
						}
					}
					if (! ok) {
						//objs.PrintAsJson(a)
						//fmt.Println("(actions item) not found")
						continue
					}

					var liveChatMessageRenderer interface{}
					liveChatMessageRenderer, ok = objs.Find(item, "liveChatTextMessageRenderer")
					if (! ok) {
						liveChatMessageRenderer, ok = objs.Find(item, "liveChatPaidMessageRenderer")
					}
					if (! ok) {
						continue
					}

					authorExternalChannelId, _ := objs.FindString(liveChatMessageRenderer, "authorExternalChannelId")
					authorName, _ := objs.FindString(liveChatMessageRenderer, "authorName", "simpleText")
					id, _ := objs.FindString(liveChatMessageRenderer, "id")
					message, _ := objs.FindString(liveChatMessageRenderer, "message", "simpleText")
					timestampUsec, _ := objs.FindString(liveChatMessageRenderer, "timestampUsec")

					if false {
						fmt.Printf("%v ", videoOffsetTimeMsec)
						fmt.Printf("%v %v %v %v %v\n", timestampUsec, authorName, authorExternalChannelId, message, id)
					}
					//fmt.Println(message)


					dbInsert(ctx, gm, db, mtx,
						id,
						timestampUsec,
						videoOffsetTimeMsec,
						authorName,
						authorExternalChannelId,
						message,
					)
				}
				//fmt.Println("------------")
			}

			if c, ok := objs.FindString(liveChatContinuation, "continuations", "timedContinuationData", "continuation"); ok {
				continuation = c
			} else if c, ok := objs.FindString(liveChatContinuation, "continuations", "liveChatReplayContinuationData", "continuation"); ok {
				continuation = c
			} else {
				err = fmt.Errorf("(liveChatContinuation continuation) not found")
				return
			}

			if t, ok := objs.FindString(liveChatContinuation, "continuations", "timedContinuationData", "timeoutMs"); ok {
				timeout, err := strconv.Atoi(t)
				if err != nil {
					timeoutMs = timeout
				}
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

		if timeoutMs < 1000 {
			if isReplay {
				timeoutMs = 1000
			} else {
				timeoutMs = 6000
			}
		}
		time.Sleep(time.Duration(timeoutMs) * time.Millisecond)
	}
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
		timestampUsec       INTEGER,
		videoOffsetTimeMsec INTEGER,
		authorName          TEXT,
		channelId           TEXT,
		message             TEXT
	)
	`)
	if err != nil {
		return
	}

	_, err = db.ExecContext(ctx, `
	CREATE UNIQUE INDEX IF NOT EXISTS comment0 ON comment(id);
	CREATE UNIQUE INDEX IF NOT EXISTS comment1 ON comment(timestampUsec);
	`)
	if err != nil {
		return
	}

	return
}

func dbInsert(ctx context.Context, gm *gorman.GoroutineManager, db *sql.DB, mtx *sync.Mutex,
	id, timestampUsec, videoOffsetTimeMsec, authorName, authorExternalChannelId, message string) {

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
		(id, timestampUsec, videoOffsetTimeMsec, authorName, channelId, message) VALUES (?,?,?,?,?,?)`

	gm.Go(func(<-chan struct{}) int {
		mtx.Lock()
		defer mtx.Unlock()

		if _, err := db.ExecContext(ctx, query,
			id, usec, offset, authorName, authorExternalChannelId, message,
		); err != nil {
			//fmt.Println(err)
			return 1
		}
		return 0
	})

	return
}

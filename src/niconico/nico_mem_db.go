package niconico

import (
	"fmt"
	"time"
	"database/sql"
)

func (hls *NicoHls) memdbOpen() (err error) {
	db, err := sql.Open("sqlite3", "file::memory:?mode=memory&cache=shared")
	if err != nil {
		return
	}

	hls.memdb = db

	err = hls.memdbCreate()
	if err != nil {
		hls.memdb.Close()
	}

	if hls.db != nil {
		rows, e := hls.db.Query(`SELECT * FROM
			(SELECT seqno, IFNULL(notfound, 0), IFNULL(size, 0) FROM media ORDER BY seqno DESC LIMIT 10) ORDER BY seqno`)
		if e != nil {
			err = e
			return
		}
		defer rows.Close()

		var found404 bool
		for rows.Next() {
			var seqno int
			var notfound bool
			var size int
			err = rows.Scan(&seqno, &notfound, &size)
			if err != nil {
				return
			}
			if notfound || size == 0 {
				hls.memdbSet404(seqno)
				found404 = true
			} else {
				hls.memdbSet200(seqno)
			}
			if (! found404) {
				hls.memdbSetStopBack(seqno)
				if hls.nicoDebug {
					fmt.Printf("memdbSetStopBack(%d)\n", seqno)
				}
			}
		}
	}

	return
}

func (hls *NicoHls) memdbCreate() (err error) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	_, err = hls.memdb.Exec(`
	CREATE TABLE IF NOT EXISTS media (
		seqno     INTEGER PRIMARY KEY NOT NULL UNIQUE,
		is200     INTEGER,
		is404     INTEGER,
		stopback  INTEGER
	)
	`)
	if err != nil {
		return
	}

	_, err = hls.memdb.Exec(`
	CREATE UNIQUE INDEX IF NOT EXISTS media0 ON media(seqno);
	`)
	if err != nil {
		return
	}

	return
}
func (hls *NicoHls) memdbSetStopBack(seqno int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Printf("%s:[WARN]memdbSetStopBack: %d(ms)\n", debug_Now(), t)
			}
		}()
	}

	_, err := hls.memdb.Exec(`
		INSERT OR IGNORE INTO media (seqno, stopback) VALUES (?, 1);
		UPDATE media SET stopback = 1 WHERE seqno=?;
	`, seqno, seqno)
	if err != nil {
		fmt.Println(err)
	}
}
func (hls *NicoHls) memdbGetStopBack(seqno int) (res bool) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Printf("%s:[WARN]memdbGetStopBack: %d(ms)\n", debug_Now(), t)
			}
		}()
	}

	hls.memdb.QueryRow("SELECT IFNULL(stopback, 0) FROM media WHERE seqno=?", seqno).Scan(&res)
	return
}
func (hls *NicoHls) memdbSet200(seqno int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Printf("%s:[WARN]memdbSet200: %d(ms)\n", debug_Now(), t)
			}
		}()
	}

	hls.memdb.Exec(`INSERT OR REPLACE INTO media (seqno, is200) VALUES (?, 1)`, seqno)
}
func (hls *NicoHls) memdbSet404(seqno int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Printf("%s:[WARN]memdbSet404: %d(ms)\n", debug_Now(), t)
			}
		}()
	}

	hls.memdb.Exec(`INSERT OR REPLACE INTO media (seqno, is404) VALUES (?, 1)`, seqno)
}
func (hls *NicoHls) memdbCheck200(seqno int) (res bool) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Printf("%s:[WARN]memdbCheck200: %d(ms)\n", debug_Now(), t)
			}
		}()
	}

	hls.memdb.QueryRow("SELECT IFNULL(is200, 0) FROM media WHERE seqno=?", seqno).Scan(&res)
	return
}
func (hls *NicoHls) memdbDelete(seqno int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Printf("%s:[WARN]memdbDelete: %d(ms)\n", debug_Now(), t)
			}
		}()
	}

	min := seqno - 100
	hls.memdb.Exec(`DELETE FROM media WHERE seqno < ?`, min)
}
func (hls *NicoHls) memdbCount() (res int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Printf("%s:[WARN]memdbCount: %d(ms)\n", debug_Now(), t)
			}
		}()
	}

	hls.memdb.QueryRow("SELECT COUNT(seqno) FROM media").Scan(&res)
	return
}
package niconico

import (
	"fmt"
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
	hls.memdb.QueryRow("SELECT IFNULL(stopback, 0) FROM media WHERE seqno=?", seqno).Scan(&res)
	return
}
func (hls *NicoHls) memdbSet200(seqno int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()
	hls.memdb.Exec(`INSERT OR REPLACE INTO media (seqno, is200) VALUES (?, 1)`, seqno)
}
func (hls *NicoHls) memdbSet404(seqno int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()
	hls.memdb.Exec(`INSERT OR REPLACE INTO media (seqno, is404) VALUES (?, 1)`, seqno)
}
func (hls *NicoHls) memdbCheck200(seqno int) (res bool) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()
	hls.memdb.QueryRow("SELECT IFNULL(is200, 0) FROM media WHERE seqno=?", seqno).Scan(&res)
	return
}
func (hls *NicoHls) memdbDelete(seqno int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()
	min := seqno - 100
	hls.memdb.Exec(`DELETE FROM media WHERE seqno < ?`, min)
}
func (hls *NicoHls) memdbCount() (res int) {
	hls.memdbMtx.Lock()
	defer hls.memdbMtx.Unlock()
	hls.memdb.QueryRow("SELECT COUNT(seqno) FROM media").Scan(&res)
	return
}
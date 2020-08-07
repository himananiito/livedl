package niconico

import (
	"fmt"
	"time"
	"os"
	"log"
	"strings"
	"database/sql"

	"path/filepath"
	"../files"
)

var SelMedia = `SELECT
	seqno, bandwidth, size, data FROM media
	WHERE IFNULL(notfound, 0) == 0 AND data IS NOT NULL
	ORDER BY seqno`

var SelComment = `SELECT
	vpos,
	date,
	date_usec,
	IFNULL(no, -1) AS no,
	IFNULL(anonymity, 0) AS anonymity,
	user_id,
	content,
	IFNULL(mail, "") AS mail,
	IFNULL(premium, 0) AS premium,
	IFNULL(score, 0) AS score,
	thread,
	IFNULL(origin, "") AS origin,
	IFNULL(locale, "") AS locale
	FROM comment
	ORDER BY date2`

func (hls *NicoHls) dbOpen() (err error) {
	db, err := sql.Open("sqlite3", hls.dbName)
	if err != nil {
		return
	}

	hls.db = db

	_, err = hls.db.Exec(`
		PRAGMA synchronous = OFF;
		PRAGMA journal_mode = WAL;
	`)
	if err != nil {
		return
	}

	err = hls.dbCreate()
	if err != nil {
		hls.db.Close()
	}
	return
}

func (hls *NicoHls) dbCreate() (err error) {
	hls.dbMtx.Lock()
	defer hls.dbMtx.Unlock()

	// table media

	_, err = hls.db.Exec(`
	CREATE TABLE IF NOT EXISTS media (
		seqno     INTEGER PRIMARY KEY NOT NULL UNIQUE,
		current   INTEGER,
		position  REAL,
		notfound  INTEGER,
		bandwidth INTEGER,
		size      INTEGER,
		data      BLOB
	)
	`)
	if err != nil {
		return
	}

	_, err = hls.db.Exec(`
	CREATE UNIQUE INDEX IF NOT EXISTS media0 ON media(seqno);
	CREATE INDEX IF NOT EXISTS media1 ON media(position);
	---- for debug ----
	CREATE INDEX IF NOT EXISTS media100 ON media(size);
	CREATE INDEX IF NOT EXISTS media101 ON media(notfound);
	`)
	if err != nil {
		return
	}

	// table comment

	_, err = hls.db.Exec(`
	CREATE TABLE IF NOT EXISTS comment (
		vpos      INTEGER NOT NULL,
		date      INTEGER NOT NULL,
		date_usec INTEGER NOT NULL,
		date2     INTEGER NOT NULL,
		no        INTEGER,
		anonymity INTEGER,
		user_id   TEXT NOT NULL,
		content   TEXT NOT NULL,
		mail      TEXT,
		premium   INTEGER,
		score     INTEGER,
		thread    TEXT,
		origin    TEXT,
		locale    TEXT,
		hash      TEXT UNIQUE NOT NULL
	)`)
	if err != nil {
		return
	}

	_, err = hls.db.Exec(`
	CREATE UNIQUE INDEX IF NOT EXISTS comment0 ON comment(hash);
	---- for debug ----
	CREATE INDEX IF NOT EXISTS comment100 ON comment(date2);
	CREATE INDEX IF NOT EXISTS comment101 ON comment(no);
	`)
	if err != nil {
		return
	}


	// kvs media

	_, err = hls.db.Exec(`
	CREATE TABLE IF NOT EXISTS kvs (
		k TEXT PRIMARY KEY NOT NULL UNIQUE,
		v BLOB
	)
	`)
	if err != nil {
		return
	}
	_, err = hls.db.Exec(`
	CREATE UNIQUE INDEX IF NOT EXISTS kvs0 ON kvs(k);
	`)
	if err != nil {
		return
	}

	//hls.__dbBegin()

	return
}

// timeshift
func (hls *NicoHls) dbSetPosition() {
	hls.dbExec(`UPDATE media SET position = ? WHERE seqno=?`,
		hls.playlist.position,
		hls.playlist.seqNo,
	)
}

// timeshift
func (hls *NicoHls) dbGetLastPosition() (res float64) {
	hls.dbMtx.Lock()
	defer hls.dbMtx.Unlock()

	hls.db.QueryRow("SELECT position FROM media ORDER BY POSITION DESC LIMIT 1").Scan(&res)
	return
}

//func (hls *NicoHls) __dbBegin() {
//	return
	///////////////////////////////////////////
	//hls.db.Exec(`BEGIN TRANSACTION`)
//}
//func (hls *NicoHls) __dbCommit(t time.Time) {
//	return
	///////////////////////////////////////////

	//// Never hls.dbMtx.Lock()
	//var start int64
	//hls.db.Exec(`COMMIT; BEGIN TRANSACTION`)
	//if t.UnixNano() - hls.lastCommit.UnixNano() > 500000000 {
	//	log.Printf("Commit: %s\n", hls.dbName)
	//}
	//hls.lastCommit = t
//}
func (hls *NicoHls) dbCommit() {
//	hls.dbMtx.Lock()
//	defer hls.dbMtx.Unlock()

//	hls.__dbCommit(time.Now())
}
func (hls *NicoHls) dbExec(query string, args ...interface{}) {
	hls.dbMtx.Lock()
	defer hls.dbMtx.Unlock()

	if hls.nicoDebug {
		start := time.Now().UnixNano()
		defer func() {
			t := (time.Now().UnixNano() - start) / (1000 * 1000)
			if t > 100 {
				fmt.Fprintf(os.Stderr, "%s:[WARN]dbExec: %d(ms):%s\n", debug_Now(), t, query)
			}
		}()
	}

	if _, err := hls.db.Exec(query, args...); err != nil {
		fmt.Printf("dbExec %#v\n", err)
		//hls.db.Exec("COMMIT")
		hls.db.Close()
		os.Exit(1)
	}
}

func (hls *NicoHls) dbKVSet(k string, v interface{}) {
	query := `INSERT OR REPLACE INTO kvs (k,v) VALUES (?,?)`
	hls.startDBGoroutine(func(sig <-chan struct{}) int {
		hls.dbExec(query, k, v)
		return OK
	})
}

func (hls *NicoHls) dbInsertReplaceOrIgnore(table string, data map[string]interface{}, replace bool) {
	var keys []string
	var qs []string
	var args []interface{}

	for k, v := range data {
		keys = append(keys, k)
		qs = append(qs, "?")
		args = append(args, v)
	}

	var replaceOrIgnore string
	if replace {
		replaceOrIgnore = "REPLACE"
	} else {
		replaceOrIgnore = "IGNORE"
	}

	query := fmt.Sprintf(
		`INSERT OR %s INTO %s (%s) VALUES (%s)`,
		replaceOrIgnore,
		table,
		strings.Join(keys, ","),
		strings.Join(qs, ","),
	)

	hls.startDBGoroutine(func(sig <-chan struct{}) int {
		hls.dbExec(query, args...)
		return OK
	})
}

func (hls *NicoHls) dbInsert(table string, data map[string]interface{}) {
	hls.dbInsertReplaceOrIgnore(table, data, false)
}
func (hls *NicoHls) dbReplace(table string, data map[string]interface{}) {
	hls.dbInsertReplaceOrIgnore(table, data, true)
}

// timeshift
func (hls *NicoHls) dbGetFromWhen() (res_from int, when float64) {
	hls.dbMtx.Lock()
	defer hls.dbMtx.Unlock()
	var date2 int64
	var no int

	hls.db.QueryRow("SELECT date2, no FROM comment ORDER BY date2 ASC LIMIT 1").Scan(&date2, &no)
	res_from = no
	if res_from <= 0 {
		res_from = 1
	}

	if date2 == 0 {
		var endTime float64
		hls.db.QueryRow(`SELECT v FROM kvs WHERE k = "endTime"`).Scan(&endTime)

		when = endTime
	} else {
		when = float64(date2) / (1000 * 1000)
	}

	return
}

func WriteComment(db *sql.DB, fileName string, skipHb bool) {

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

	for rows.Next() {
		var vpos      int64
		var date      int64
		var date_usec int64
		var no        int64
		var anonymity int64
		var user_id   string
		var content   string
		var mail      string
		var premium   int64
		var score     int64
		var thread    string
		var origin    string
		var locale    string
		err = rows.Scan(
			&vpos      ,
			&date      ,
			&date_usec ,
			&no        ,
			&anonymity ,
			&user_id   ,
			&content   ,
			&mail      ,
			&premium   ,
			&score     ,
			&thread    ,
			&origin    ,
			&locale    ,
		)
		if err != nil {
			log.Println(err)
			return
		}

		// skip /hb
		if (premium > 1) && skipHb && strings.HasPrefix(content, "/hb ") {
			continue
		}

		if (vpos < 0) {
			continue
		}

		line := fmt.Sprintf(
			`<chat thread="%s" vpos="%d" date="%d" date_usec="%d" user_id="%s"`,
			thread,
			vpos,
			date,
			date_usec,
			user_id,
		)

		if no >= 0 {
			line += fmt.Sprintf(` no="%d"`, no)
		}
		if anonymity != 0 {
			line += fmt.Sprintf(` anonymity="%d"`, anonymity)
		}
		if mail != "" {
			mail = strings.Replace(mail, `"`, "&quot;", -1)
			mail = strings.Replace(mail, "&", "&amp;", -1)
			mail = strings.Replace(mail, "<", "&lt;", -1)
			line += fmt.Sprintf(` mail="%s"`, mail)
		}
		if origin != "" {
			origin = strings.Replace(origin, `"`, "&quot;", -1)
			origin = strings.Replace(origin, "&", "&amp;", -1)
			origin = strings.Replace(origin, "<", "&lt;", -1)
			line += fmt.Sprintf(` origin="%s"`, origin)
		}
		if premium != 0 {
			line += fmt.Sprintf(` premium="%d"`, premium)
		}
		if score != 0 {
			line += fmt.Sprintf(` score="%d"`, score)
		}
		if locale != "" {
			locale = strings.Replace(locale, `"`, "&quot;", -1)
			locale = strings.Replace(locale, "&", "&amp;", -1)
			locale = strings.Replace(locale, "<", "&lt;", -1)
			line += fmt.Sprintf(` locale="%s"`, locale)
		}
		line += ">"
		content = strings.Replace(content, "&", "&amp;", -1)
		content = strings.Replace(content, "<", "&lt;", -1)
		line += content
		line += "</chat>"
		fmt.Fprintf(f, "%s\r\n", line)
	}
	fmt.Fprintf(f, "%s\r\n", `</packet>`)
}

// ts
func (hls *NicoHls) dbGetLastMedia(i int) (res []byte) {
	hls.dbMtx.Lock()
	defer hls.dbMtx.Unlock()
	hls.db.QueryRow("SELECT data FROM media WHERE seqno = ?", i).Scan(&res)
	return
}
func (hls *NicoHls) dbGetLastSeqNo() (res int64) {
	hls.dbMtx.Lock()
	defer hls.dbMtx.Unlock()
	hls.db.QueryRow("SELECT seqno FROM media ORDER BY seqno DESC LIMIT 1").Scan(&res)
	return
}
package niconico

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/himananiito/livedl/options"
)

func readFirefoxProfiles(file string) (profiles map[string]string, err error) {

	profiles = make(map[string]string)
	data, err := os.Open(file)
	if err != nil {
		err = fmt.Errorf("cookies from browser failed: profile.ini can't read")
		return
	}
	defer data.Close()

	var key string
	flag := false
	scanner := bufio.NewScanner(data)
	for scanner.Scan() {
		//fmt.Println(scanner.Text())
		if strings.Index(scanner.Text(), "[Profile") >= 0 {
			flag = true
			continue
		}
		if flag {
			if ma := regexp.MustCompile(`^Name=(.+)$`).FindStringSubmatch(scanner.Text()); len(ma) > 0 {
				key = ma[1]
				continue
			}
			if ma := regexp.MustCompile(`^Path=(.+)$`).FindStringSubmatch(scanner.Text()); len(ma) > 0 {
				profiles[key] = ma[1]
				flag = false
				continue
			}
		}
	}
	return
}

func NicoBrowserCookies(opt options.Option) (sessionkey string, err error) {

	sessionkey = ""
	var profname string
	var dbfile string
	profiles := make(map[string]string)

	fmt.Println("NicoCookies:", opt.NicoCookies)
	if ma := regexp.MustCompile(`^([^:]+):?(.*)$`).FindStringSubmatch(opt.NicoCookies); len(ma) > 0 {
		profname = ma[2]
	}
	if len(profname) < 1 {
		profname = "default-release"
	}
	//fmt.Println("Profile:",profname)

	if strings.Index(profname, "cookies.sqlite") < 0 {
		//profiles.iniを開く
		switch runtime.GOOS {
		case "windows":
			dbfile = os.Getenv("APPDATA") + "/Mozilla/Firefox"
			_, err = os.Stat(dbfile)
			if os.IsNotExist(err) {
				err = fmt.Errorf("cookies from browser failed: firefox profiles not found")
				return
			}
		case "darwin":
			dbfile = os.Getenv("HOME") + "/Library/Application Support/Firefox"
			_, err = os.Stat(dbfile)
			if os.IsNotExist(err) {
				dbfile = os.Getenv("HOME") + "/.mozilla/firefox"
				_, err = os.Stat(dbfile)
				if os.IsNotExist(err) {
					err = fmt.Errorf("cookies from browser failed: firefox profiles not found")
					return
				}
			}
		default:
			dbfile = os.Getenv("HOME") + "/snap/firefox/common/.mozilla/firefox"
			_, err = os.Stat(dbfile)
			if os.IsNotExist(err) {
				dbfile = os.Getenv("HOME") + "/.mozilla/firefox"
				_, err = os.Stat(dbfile)
				if os.IsNotExist(err) {
					err = fmt.Errorf("cookies from browser failed: firefox profiles not found")
					return
				}
			}
		}

		profiles, err = readFirefoxProfiles(dbfile + "/profiles.ini")
		if len(profiles) <= 0 {
			err = fmt.Errorf("cookies from browser failed: profiles not found")
			return
		}
		if _, ok := profiles[profname]; !ok {
			err = fmt.Errorf("cookies from browser failed: profiles not found")
			return
		}
		if file, _ := filepath.Glob(dbfile + "/" + profiles[profname] + "/cookies.sqlite"); len(file) > 0 {
			dbfile = file[0]
		}
		//fmt.Println(dbfile)

		if strings.Index(dbfile, "cookies.sqlite") < 0 {
			err = fmt.Errorf("cookies from browser failed: cookies not found")
			return
		}
	} else {
		dbfile = profname
	}
	fmt.Println("cookiefile:", dbfile)

	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		err = fmt.Errorf("cookies from browser failed: cookie not found")
		return
	}
	query := `SELECT name, value FROM moz_cookies WHERE host='.nicovideo.jp'`
	rows, err := db.Query(query)
	if err != nil {
		log.Println(err)
		db.Close()
		return
	}
	defer rows.Close()

	result := ""
	for rows.Next() {
		var name string
		var value string
		var dest = []interface{}{
			&name,
			&value,
		}
		err = rows.Scan(dest...)
		if err != nil {
			log.Println(err)
			db.Close()
			return
		}
		result += name + "=" + value + "; "
	}
	db.Close()

	//Cookieからuser_sessionの値を読み込む
	if ma := regexp.MustCompile(`user_session=(user_session_.+?);`).FindStringSubmatch(result); len(ma) > 0 {
		fmt.Println("session_key: ", string(ma[1]))
		sessionkey = string(ma[1])
		fmt.Println("cookies from browser get success")
	} else {
		err = fmt.Errorf("cookies from browser failed: session_key not found")
	}

	return
}

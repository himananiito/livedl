package youtube

import (
	"fmt"
	"net/http"
	"io/ioutil"
	"bufio"
	"regexp"
	"encoding/json"
	"html"
	"../obj"
	"../files"
	"../procs/streamlink"
)

func Record(id string) (err error) {
	uri := fmt.Sprintf("https://www.youtube.com/watch?v=%s", id)
	req, _ := http.NewRequest("GET", uri, nil)

	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	dat, _ := ioutil.ReadAll(resp.Body)

	var a interface{}

	re := regexp.MustCompile(`\Wytplayer\.config\p{Zs}*=\p{Zs}*({.*?})\p{Zs}*;`)
	if ma := re.FindSubmatch(dat); len(ma) > 0 {
		str := html.UnescapeString(string(ma[1]))
		if err = json.Unmarshal([]byte(str), &a); err != nil {
			fmt.Println(str)
			fmt.Println(err)
			return
		}
	} else {
		fmt.Println("ytplayer.config not found")
		return
	}

	// debug print
	//obj.PrintAsJson(a)

	title, ok := obj.FindString(a, "args", "title")
	if (! ok) {
		fmt.Println("title not found")
		return
	}

	name := fmt.Sprintf("%s_%s.mp4", title, id)
	name = files.ReplaceForbidden(name)
	name, err = files.GetFileNameNext(name)
	if err != nil {
		fmt.Println(err)
		return
	}

	cmd, stderr, err := streamlink.Open(uri, "best", "--retry-max", "10", "-o", name)
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		rdr := bufio.NewReader(stderr)
		re := regexp.MustCompile(`(Written.*?)\s*\z`)
		for {
			s, err := rdr.ReadString('\r')
			if err != nil {
				return
			}
			if ma := re.FindStringSubmatch(s); len(ma) > 1 {
				fmt.Printf("%s: %s\n", name, ma[1])
			}
		}
	}()

	cmd.Wait()

	return
}

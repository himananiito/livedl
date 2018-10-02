package httpbase

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"net/http"
	"net/url"
	"time"
	"errors"
	"../buildno"
	"../defines"
)

func GetUserAgent() string {
	return fmt.Sprintf(
		"livedl/%s (contact: twitter=%s, email=%s)",
		buildno.GetBuildNo(),
		defines.Twitter,
		defines.Email,
	)
}

var Client = &http.Client{
	Timeout: time.Duration(5) * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) (err error) {
		if req != nil && via != nil && len(via) > 0 {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			req.Header = via[0].Header
		}
		return nil
	},
}
func httpBase(method, uri string, header map[string]string, body io.Reader) (resp *http.Response, err, neterr error) {

	req, err := http.NewRequest(method, uri, body)
	if err != nil {
		return
	}

	req.Header.Set("User-Agent", GetUserAgent())

	for k, v := range header {
		req.Header.Set(k, v)
	}

	resp, neterr = Client.Do(req)
	if neterr != nil {
		return
	}
	return
}
func Get(uri string, header map[string]string) (*http.Response, error, error) {
	return httpBase("GET", uri, header, nil)
}
func PostForm(uri string, header map[string]string, val url.Values) (*http.Response, error, error) {
	if header == nil {
		header = make(map[string]string)
	}
	header["Content-Type"] = "application/x-www-form-urlencoded; charset=utf-8"
	return httpBase("POST", uri, header, strings.NewReader(val.Encode()))
}

func GetBytes(uri string, header map[string]string) (code int, buff []byte, err, neterr error) {
	resp, err, neterr := Get(uri, header)
	if err != nil {
		return
	}
	if neterr != nil {
		return
	}
	defer resp.Body.Close()

	buff, neterr = ioutil.ReadAll(resp.Body)
	if neterr != nil {
		return
	}

	code = resp.StatusCode

	return
}
package httpbase

import (
	"fmt"
	"io"
	"strings"
	"net/http"
	"net/url"
	"time"
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
}
func httpBase(method, uri string, header map[string]string, body io.Reader) (resp *http.Response, err, neterr error) {

	req, err := http.NewRequest(method, uri, body)
	if err != nil {
		return
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}

	req.Header.Set("User-Agent", GetUserAgent())

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

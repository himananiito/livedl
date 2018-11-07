package httpbase

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"net/http"
	"net/url"
	"time"
	"errors"
	"encoding/json"
	"encoding/pem"
	"bytes"

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

func checkTransport() bool {
	if Client.Transport == nil {
		Client.Transport = &http.Transport{}
	}
	switch Client.Transport.(type) {
	case *http.Transport:
		return true
	}
	return false
}
func checkTLSClientConfig() bool {
	if !checkTransport() {
		return false
	}

	if Client.Transport.(*http.Transport).TLSClientConfig == nil {
		Client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{}
	}

	return true
}
func SetRootCA(file string) (err error) {
	if ! checkTLSClientConfig() {
		err = fmt.Errorf("SetRootCA: check failed")
		return
	}

	dat, err := ioutil.ReadFile(file)
	if err != nil {
		return
	}

	// try decode pem
	var nDecode int
	for len(dat) > 0 {
		block, d := pem.Decode(dat)
		if block == nil {
			break
		}
		dat = d
		nDecode++
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}
		addCert(block.Bytes)
	}
	if nDecode < 1 {
		addCert(dat)
	}

	return
}
func addCert(dat []byte) (err error) {
	certs, err := x509.ParseCertificates(dat)
	if err != nil {
		return
	}
	if certs == nil {
		err = fmt.Errorf("ParseCertificates failed")
		return
	}

	if len(certs) > 0 {
		if Client.Transport.(*http.Transport).TLSClientConfig.RootCAs == nil {
			Client.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
		}
	}

	for _, cert := range certs {
		Client.Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(cert)
	}
	return
}

func SetSkipVerify(skip bool) (err error) {
	if checkTLSClientConfig() {
		Client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = skip
	} else {
		err = fmt.Errorf("SetSkipVerify(%#v): check failed", skip)
	}
	return
}
func SetProxy(rawurl string) (err error) {
	if ! checkTransport() {
		return fmt.Errorf("SetProxy(%#v): check failed", rawurl)
	}

	u, err := url.Parse(rawurl)
	if err != nil {
		return
	}
	Client.Transport.(*http.Transport).Proxy = http.ProxyURL(u)
	return
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
		if strings.Contains(neterr.Error(), "x509: certificate signed by unknown") {
			fmt.Println(neterr)
			os.Exit(10)
		}
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
func reqJson(method, uri string, header map[string]string, data interface{}) (
	*http.Response, error, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err, nil
	}

	if header == nil {
		header = make(map[string]string)
	}
	header["Content-Type"] = "application/json"

	return httpBase(method, uri, header, bytes.NewReader(encoded))
}
func PostJson(uri string, header map[string]string, data interface{}) (*http.Response, error, error) {
	return reqJson("POST", uri, header, data)
}
func PutJson(uri string, header map[string]string, data interface{}) (*http.Response, error, error) {
	return reqJson("PUT", uri, header, data)
}
func PostData(uri string, header map[string]string, data io.Reader) (*http.Response, error, error) {
	if header == nil {
		header = make(map[string]string)
	}
	return httpBase("POST", uri, header, data)
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
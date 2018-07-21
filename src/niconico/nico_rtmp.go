package niconico

import (
	"fmt"
	"encoding/xml"
	"io/ioutil"
	"regexp"
	"strings"
	"net/url"
	"sync"
	"log"
	"time"
	"../rtmps"
	"../amf"
	"../options"
	"../files"
	"../httpbase"
)

type Content struct {
	Id string `xml:"id,attr"`
	Text string `xml:",chardata"`
}
type Tickets struct {
	Name string `xml:"name,attr"`
	Text string `xml:",chardata"`
}
type Status struct {
	Title                   string  `xml:"stream>title"`
	CommunityId             string  `xml:"stream>default_community"`
	Id                      string  `xml:"stream>id"`
	Provider                string  `xml:"stream>provider_type"`
	IsArchive               bool    `xml:"stream>archive"`
	IsArchivePlayerServer   bool    `xml:"stream>is_archiveplayserver"`
	Ques                  []string  `xml:"stream>quesheet>que"`
	Contents              []Content `xml:"stream>contents_list>contents"`
	IsPremium               bool    `xml:"user>is_premium"`
	Url                     string  `xml:"rtmp>url"`
	Ticket                  string  `xml:"rtmp>ticket"`
	Tickets               []Tickets `xml:"tickets>stream"`
	ErrorCode               string  `xml:"error>code"`
	streams               []Stream
	chStream                chan struct{}
	wg                     *sync.WaitGroup
}
type Stream struct {
	originUrl string
	streamName string
	originTicket string
}
func (status *Status) quesheet() {
	stream := make(map[string][]Stream)
	playType := make(map[string]string)

	// timeshift; <quesheet> tag
	re_pub := regexp.MustCompile(`\A/publish\s+(\S+)\s+(?:(\S+?),)?(\S+?)(?:\?(\S+))?\z`)
	re_play := regexp.MustCompile(`\A/play\s+(\S+)\s+(\S+)\z`)

	for _, q := range status.Ques {
		// /publish lv* /content/*/lv*_*_1_*.f4v
		if ma := re_pub.FindStringSubmatch(q); len(ma) >= 5 {
			stream[ma[1]] = append(stream[ma[1]], Stream{
				originUrl: ma[2],
				streamName: ma[3],
				originTicket: ma[4],
			})

		// /play ...
		} else if ma := re_play.FindStringSubmatch(q); len(ma) > 0 {
			// /play case:sp:rtmp:lv*_s_lv*,mobile:rtmp:lv*_s_lv*_sub1,premium:rtmp:lv*_s_lv*_sub1,default:rtmp:lv*_s_lv* main
			if strings.HasPrefix(ma[1], "case:") {
				s0 := ma[1]
				s0 = strings.TrimPrefix(s0, "case:")
				cases := strings.Split(s0, ",")
				// sp:rtmp:lv*_s_lv*
				re := regexp.MustCompile(`\A(\S+?):rtmp:(\S+?)\z`)
				for _, c := range cases {
					if ma := re.FindStringSubmatch(c); len(ma) > 0 {
						playType[ma[1]] = ma[2]
					}
				}

			// /play rtmp:lv* main
			} else {
				re := regexp.MustCompile(`\Artmp:(\S+?)\z`)
				if ma := re.FindStringSubmatch(ma[1]); len(ma) > 0 {
					playType["default"] = ma[1]
				}
			}
		}
	}

	pt, ok := playType["premium"]
	if ok && status.IsPremium {
		s, ok := stream[ pt ]
		if ok {
			status.streams = s
		}
	} else {
		pt, ok := playType["default"]
		if ok {
			s, ok := stream[ pt ]
			if ok {
				status.streams = s
			}
		}
	}
}
func (status *Status) initStreams() {

	if len(status.streams) > 0 {
		return
	}

	//if status.isOfficialLive() {
		status.contentsOfficialLive()
	//} else if status.isLive() {
		status.contentsNonOfficialLive()
	//} else {
		status.quesheet()
	//}

	return
}
func (status *Status) getFileName(index int) (name string) {
	if len(status.streams) == 1 {
		//name = fmt.Sprintf("%s.flv", status.Id)
		name = fmt.Sprintf("%s-%s-%s.flv", status.Id, status.CommunityId, status.Title)
	} else if len(status.streams) > 1 {
		//name = fmt.Sprintf("%s-%d.flv", status.Id, 1 + index)
		name = fmt.Sprintf("%s-%s-%s#%d.flv", status.Id, status.CommunityId, status.Title, 1 + index)
	} else {
		log.Fatalf("No stream")
	}
	name = files.ReplaceForbidden(name)
	return
}
func (status *Status) contentsNonOfficialLive() {
	re := regexp.MustCompile(`\A(?:rtmp:)?(rtmp\w*://\S+?)(?:,(\S+?)(?:\?(\S+))?)?\z`)

	// Live (not timeshift); <contents_list> tag
	for _, c := range status.Contents {
		if ma := re.FindStringSubmatch(c.Text); len(ma) > 0 {
			status.streams = append(status.streams, Stream{
				originUrl: ma[1],
				streamName: ma[2],
				originTicket: ma[3],
			})
		}
	}

}
func (status *Status) contentsOfficialLive() {

	tickets := make(map[string] string)
	for _, t := range status.Tickets {
		tickets[t.Name] = t.Text
	}

	for _, c := range status.Contents {
		if strings.HasPrefix(c.Text, "case:") {
			c.Text = strings.TrimPrefix(c.Text, "case:")

			for _, c := range strings.Split(c.Text, ",") {
				c, e := url.PathUnescape(c)
				if e != nil {
					fmt.Printf("%v\n", e)
				}

				re := regexp.MustCompile(`\A(\S+?):(?:limelight:|akamai:)?(\S+),(\S+)\z`)
				if ma := re.FindStringSubmatch(c); len(ma) > 0 {
					fmt.Printf("\n%#v\n", ma)
					switch ma[1] {
						default:
							fmt.Printf("unknown contents case %#v\n", ma[1])
						case "mobile":
						case "middle":
						case "default":
							status.Url = ma[2]
							t, ok := tickets[ma[3]]
							if (! ok) {
								fmt.Printf("not found %s\n", ma[3])
							}
							fmt.Printf("%s\n", t)
							status.streams = append(status.streams, Stream{
								streamName: ma[3],
								originTicket: t,
							})
					}
				}
			}
		}
	}
}

func (status *Status) relayStreamName(i, offset int) (s string) {
	s = regexp.MustCompile(`[^/\\]+\z`).FindString(status.streams[i].streamName)
	if offset >= 0 {
		s += fmt.Sprintf("_%d", offset)
	}
	return
}

func (status *Status) streamName(i, offset int) (name string, err error) {
	if status.isOfficialLive() {
		if i >= len(status.streams) {
			err = fmt.Errorf("(status *Status) streamName(i int): Out of index: %d\n", i)
			return
		}

		name = status.streams[i].streamName
		if status.streams[i].originTicket != "" {
			name += "?" + status.streams[i].originTicket
		}
		return

	} else if status.isOfficialTs() {
		name = status.streams[i].streamName
		name = regexp.MustCompile(`(?i:\.flv)$`).ReplaceAllString(name, "")
		if regexp.MustCompile(`(?i:\.(?:f4v|mp4))$`).MatchString(name) {
			name = "mp4:" + name
		} else if regexp.MustCompile(`(?i:\.raw)$`).MatchString(name) {
			name = "raw:" + name
		}

	} else {
		name = status.relayStreamName(i, offset)
	}

	return
}
func (status *Status) tcUrl() (url string, err error) {
	if status.Url != "" {
		url = status.Url
		return
	} else {
		status.contentsOfficialLive()
	}

	if status.Url != "" {
		url = status.Url
		return
	}

	err = fmt.Errorf("tcUrl not found")
	return
}
func (status *Status) isTs() bool {
	return status.IsArchive
}
func (status *Status) isLive() bool {
	return (! status.IsArchive)
}
func (status *Status) isOfficialLive() bool {
	return (status.Provider == "official") && (! status.IsArchive)
}
func (status *Status) isOfficialTs() bool {
	if status.IsArchive {
		switch status.Provider {
		case "official": return true
		case "channel": return status.IsArchivePlayerServer
		}
	}
	return false
}

func (st Stream) relayStreamName(offset int) (s string) {
	s = regexp.MustCompile(`[^/\\]+\z`).FindString(st.streamName)
	if offset >= 0 {
		s += fmt.Sprintf("_%d", offset)
	}
	return
}
func (st Stream) noticeStreamName(offset int) (s string) {
	s = st.streamName
	s = regexp.MustCompile(`(?i:\.flv)$`).ReplaceAllString(s, "")
	if regexp.MustCompile(`(?i:\.(?:f4v|mp4))$`).MatchString(s) {
		s = "mp4:" + s
	} else if regexp.MustCompile(`(?i:\.raw)$`).MatchString(s) {
		s = "raw:" + s
	}

	if st.originTicket != "" {
		s += "?" + st.originTicket
	}

	return
}

func (status *Status) recStream(index int, opt options.Option) (err error) {
	defer func(){
		<-status.chStream
		status.wg.Done()
	}()

	stream := status.streams[index]

	tcUrl, err := status.tcUrl()
	if err != nil {
		return
	}

	rtmp, err := rtmps.NewRtmp(
		// tcUrl
		tcUrl,
		// swfUrl
		"http://live.nicovideo.jp/nicoliveplayer.swf?180116154229",
		// pageUrl
		"http://live.nicovideo.jp/watch/" + status.Id,
		// option
		status.Ticket,
	)
	if err != nil {
		return
	}
	defer rtmp.Close()


	fileName, err := files.GetFileNameNext(status.getFileName(index))
	if err != nil {
		return
	}
	rtmp.SetFlvName(fileName)


	tryRecord := func() (incomplete bool, err error) {

		if err = rtmp.Connect(); err != nil {
			return
		}

		// default: 2500000
		//if err = rtmp.SetPeerBandwidth(100*1000*1000, 0); err != nil {
		if err = rtmp.SetPeerBandwidth(2500000, 0); err != nil {
			fmt.Printf("SetPeerBandwidth: %v\n", err)
			return
		}

		if err = rtmp.WindowAckSize(2500000); err != nil {
			fmt.Printf("WindowAckSize: %v\n", err)
			return
		}

		if err = rtmp.CreateStream(); err != nil {
			fmt.Printf("CreateStream %v\n", err)
			return
		}

		if err = rtmp.SetBufferLength(0, 2000); err != nil {
			fmt.Printf("SetBufferLength: %v\n", err)
			return
		}

		var offset int
		if status.IsArchive {
			offset = 0
		} else {
			offset = -2
		}

		if status.isOfficialTs() {
			for i := 0; true; i++ {
				if i > 30 {
					err = fmt.Errorf("sendFileRequest: No response")
					return
				}
				data, e := rtmp.Command(
					"sendFileRequest", []interface{} {
					nil,
					amf.SwitchToAmf3(),
					[]string{
						stream.streamName,
					},
				})
				if e != nil {
					err = e
					return
				}

				var resCnt int
				switch data.(type) {
				case map[string]interface{}:
					resCnt = len(data.(map[string]interface{}))
				case map[int]interface{}:
					resCnt = len(data.(map[int]interface{}))
				case []interface{}:
					resCnt = len(data.([]interface{}))
				case []string:
					resCnt = len(data.([]string))
				}
				if resCnt > 0 {
					break
				}
				time.Sleep(10 * time.Second)
			}

		} else if (! status.isOfficialLive()) {
			// /publishの第二引数
			// streamName(param1:String)
			// 「,」で区切る
			// ._originUrl, streamName(playStreamName)
			// streamName に、「?」がついてるなら originTickt となる
			// streamName の.flvは削除する
			// streamNameが/\.(f4v|mp4)$/iなら、頭にmp4:をつける
			// /\.raw$/iなら、raw:をつける。
			// relayStreamName: streamNameの頭からスラッシュまでを削除したもの

			_, err = rtmp.Command(
				"nlPlayNotice", []interface{} {
				nil,
				// _connection.request.originUrl
				stream.originUrl,

				// this._connection.request.playStreamRequest
				// originticket あるなら
				// playStreamName ? this._originTicket
				// 無いなら playStreamName
				stream.noticeStreamName(offset),

				// var _loc1_:String = this._relayStreamName;
				// if(this._offset != -2)
				// {
				// _loc1_ = _loc1_ + ("_" + this.offset);
				// }
				// user nama: String 'lvxxxxxxxxx'
				// user kako: lvxxxxxxxxx_xxxxxxxxxxxx_1_xxxxxx.f4v_0
				stream.relayStreamName(offset),

				// seek offset
				// user nama: -2, user kako: 0
				offset,
			})
			if err != nil {
				fmt.Printf("nlPlayNotice %v\n", err)
				return
			}
		}

		if err = rtmp.SetBufferLength(1, 3600 * 1000); err != nil {
			fmt.Printf("SetBufferLength: %v\n", err)
			return
		}

		// No return
		rtmp.SetFixAggrTimestamp(true)

		// user kako: lv*********_************_*_******.f4v_0
		// official or channel ts: mp4:/content/********/lv*********_************_*_******.f4v
		//if err = rtmp.Play(status.origin.playStreamName(status.isTsOfficial(), offset)); err != nil {
		streamName, err := status.streamName(index, offset)
		if err != nil {
			return
		}

		if status.isOfficialTs() {
			ts := rtmp.GetTimestamp()
			if ts > 1000 {
				err = rtmp.PlayTime(streamName, ts - 1000)
			} else {
				err = rtmp.PlayTime(streamName, -5000)
			}

		} else if status.isTs() {
			rtmp.SetFlush(true)
			err = rtmp.PlayTime(streamName, -5000)

		} else {
			err = rtmp.Play(streamName)
		}
		if err != nil {
			fmt.Printf("Play: %v\n", err)
			return
		}

		// Non-recordedなタイムシフトでseekしても、timestampが変わるだけで
		// 最初からの再生となってしまうのでやらないこと

		// 公式のタイムシフトでSeekしてもタイムスタンプがおかしい

		if opt.NicoTestTimeout > 0 {
			// test mode
			_, incomplete, err = rtmp.WaitTest(opt.NicoTestTimeout)
		} else {
			// normal mode
			_, incomplete, err = rtmp.Wait()
		}
		return
	} // end func

	//ticketTime := time.Now().Unix()
	//rtmp.SetNoSeek(false)
	for i := 0; i < 10; i++ {
		incomplete, e := tryRecord()
		if e != nil {
			err = e
			fmt.Printf("%v\n", e)
			return
		} else if incomplete && status.isOfficialTs() {
			fmt.Println("incomplete")
			time.Sleep(3 * time.Second)

			// update ticket
			if true {
				//if time.Now().Unix() > ticketTime + 60 {
					//ticketTime = time.Now().Unix()
					if ticket, e := getTicket(opt); e != nil {
						err = e
						return
					} else {
						rtmp.SetConnectOpt(ticket)
					}
				//}
			}

			continue
		}
		break
	}

	fmt.Printf("done\n")
	return
}

func (status *Status) recAllStreams(opt options.Option) (err error) {

	status.initStreams()

	var MaxConn int
	if opt.NicoRtmpMaxConn == 0 {
		if status.isOfficialTs() {
			MaxConn = 1
		} else {
			MaxConn = 4
		}
	} else if opt.NicoRtmpMaxConn < 0 {
		MaxConn = 1
	} else {
		MaxConn = opt.NicoRtmpMaxConn
	}

	status.wg = &sync.WaitGroup{}
	status.chStream = make(chan struct{}, MaxConn)

	ticketTime := time.Now().Unix()

	for index, _ := range status.streams {
		if opt.NicoRtmpIndex != nil {
			if tes, ok := opt.NicoRtmpIndex[index]; !ok || !tes {
				continue
			}
		}

		// blocks here
		status.chStream <- struct{}{}
		status.wg.Add(1)

		go status.recStream(index, opt)

		now := time.Now().Unix()
		if now > ticketTime + 60 {
			ticketTime = now
			if ticket, e := getTicket(opt); e != nil {
				err = e
				return
			} else {
				status.Ticket = ticket
			}
		}
	}

	status.wg.Wait()

	return
}

func getTicket(opt options.Option) (ticket string, err error) {
	status, notLogin, err := getStatus(opt)
	if err != nil {
		return
	}
	if status.Ticket != "" {
		ticket = status.Ticket
	} else {
		if notLogin {
			err = fmt.Errorf("notLogin")
		} else {
			err = fmt.Errorf("Ticket not found")
		}
	}
	return
}
func getStatus(opt options.Option) (status *Status, notLogin bool, err error) {
	var uri string

	// experimental
	if opt.NicoStatusHTTPS {
		uri = fmt.Sprintf("https://ow.live.nicovideo.jp/api/getplayerstatus?v=%s", opt.NicoLiveId)
	} else {
		uri = fmt.Sprintf("http://watch.live.nicovideo.jp/api/getplayerstatus?v=%s", opt.NicoLiveId)
	}

	header := make(map[string]string, 4)
	if opt.NicoSession != "" {
		header["Cookie"] = "user_session=" + opt.NicoSession
	}

	// experimental
	//if opt.NicoStatusHTTPS {
	//	req.Header.Set("User-Agent", "Niconico/1.0 (Unix; U; iPhone OS 10.3.3; ja-jp; nicoiphone; iPhone5,2) Version/6.65")
	//}

	resp, err, neterr := httpbase.Get(uri, header)
	if err != nil {
		return
	}
	if neterr != nil {
		err = neterr
		return
	}
	defer resp.Body.Close()

	dat, _ := ioutil.ReadAll(resp.Body)
	status = &Status{}
	err = xml.Unmarshal(dat, status)
	if err != nil {
		//fmt.Println(string(dat))
		fmt.Printf("error: %v", err)
		return
	}

	switch status.ErrorCode {
	case "":
	case "notlogin":
		notLogin = true
	default:
		err = fmt.Errorf("Error code: %s\n", status.ErrorCode)
		return
	}

	return
}

func NicoRecRtmp(opt options.Option) (notLogin bool, err error) {
	status, notLogin, err := getStatus(opt)
	if err != nil {
		return
	}
	if notLogin {
		return
	}

	status.recAllStreams(opt)
	return
}

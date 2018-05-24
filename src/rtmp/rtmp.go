package rtmp

import (
	"net"
	"fmt"
	"bytes"
	"math/rand"
	"time"
	"io"
	"io/ioutil"
	"regexp"
	"../amf"
	"../flv"
	"../obj"
	"../files"
)

type chunkInfo struct {
	timestampField int
	timestampDelta int
	timestampActual int
	msgLength int
	msgTypeId int
	msgStreamId int
}

type Rtmp struct {
	proto string
	address string
	app string
	tcUrl string
	swfUrl string
	pageUrl string
	connectOpt []interface{}

	conn *net.TCPConn
	chunkSizeSend int
	chunkSizeRecv int
	transactionId int
	windowSize int
	chunkInfo map[int] chunkInfo

	readCount int
	totalReadBytes int
	isRecorded bool

	timestamp int
	duration int

	flvName string
	flv *flv.Flv

	fixAggrTimestamp bool
	streamId int
	nextLogTs int

	VideoExists bool
}

func NewRtmp(tc, swf, page string, opt... interface{})(rtmp *Rtmp, err error) {
	re := regexp.MustCompile(`\A(\w+)://([^/\s]+)/(\S+)\z`)
	mstr := re.FindStringSubmatch(tc)
	if mstr == nil {
		err = fmt.Errorf("tcUrl incorrect: %v", tc)
		return
	}

	rtmp = &Rtmp{
		proto: mstr[1],
		address: mstr[2],
		app: mstr[3],
		tcUrl: tc,
		swfUrl: swf,
		pageUrl: page,
		connectOpt: opt,

		chunkSizeSend: 128,
		chunkSizeRecv: 128,
		transactionId: 1,
		windowSize: 2500000,
		chunkInfo: make(map[int] chunkInfo),
	}

	err = rtmp.Connect()

	return
}
func (rtmp *Rtmp) Connect() (err error) {
	if rtmp.conn != nil {
		rtmp.conn.Close()
		rtmp.conn = nil
	}
	err = rtmp.connect(
		rtmp.app,
		rtmp.tcUrl,
		rtmp.swfUrl,
		rtmp.pageUrl,
		rtmp.connectOpt...,
	)
	return
}
func (rtmp *Rtmp) connect(app, tc, swf, page string, opt... interface{}) (err error) {

	raddr, err := net.ResolveTCPAddr("tcp", rtmp.address)
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}

	switch rtmp.proto {
		case "rtmp":
			conn, e := net.DialTCP("tcp", nil, raddr)
			if e != nil {
				err = e
				return
			}
			rtmp.conn = conn

		default:
			err = fmt.Errorf("Unknown protocol: %v", rtmp.proto)
			return
	}

	err = handshake(rtmp.conn)
	if err != nil {
		rtmp.conn.Close()
		return
	}

	var data []interface{}
	data = append(data, map[string]interface{} {
		"app"           : app,
		"flashVer"      : "WIN 29,0,0,113",
		"swfUrl"        : swf,
		"tcUrl"         : tc,
		"fpad"          : false,
		"capabilities"  : 239,
		"audioCodecs"   : 0xFFF, //3575,
		"videoCodecs"   : 0xFF, //252,
		"videoFunction" : 1,
		"pageUrl"       : page,
		"objectEncoding": 3,
	})

	for _, o := range opt {
		data = append(data, o)
	}

	err = rtmp.Command("connect", data)

	return
}

func (rtmp *Rtmp) WaitPause() (done, incomplete bool, err error) {
	var pause bool
	for {
		done, incomplete, _, pause, err = rtmp.recvChunk(-1, true)
		if err != nil || done || incomplete || pause {
			return
		}
	}
	return
}
func (rtmp *Rtmp) Wait() (done, incomplete bool, err error) {
	for {
		done, incomplete, _, _, err = rtmp.recvChunk(-1, false)
		if err != nil {
			return
		}
		if done || incomplete {
			return
		}
	}
	return
}
func (rtmp *Rtmp) waitCommand(findTrId int) (done, incomplete bool, err error) {
	var trFound bool
	for {
		done, incomplete, trFound, _, err = rtmp.recvChunk(findTrId, false)
		if err != nil || done || incomplete || trFound {
			return
		}
	}
	return
}
func (rtmp *Rtmp) SetFlvName(name string) {
	rtmp.flvName = name
}
func (rtmp *Rtmp) openFlv(incr bool) (err error) {
	if rtmp.flvName == "" {
		err = fmt.Errorf("FLV file name not set: call SetFlvName(string)")
		return
	}
	var fileName string
	if incr {
		if fileName, err = files.GetFileNameNext(rtmp.flvName); err != nil {
			return
		}
	} else {
		fileName = rtmp.flvName
	}
	flv, err := flv.Open(fileName)
	if err != nil {
		return
	}
	rtmp.flv = flv
	return
}
func (rtmp *Rtmp) GetTimestamp() int {
	return rtmp.timestamp
}
func (rtmp *Rtmp) writeMetaData(body map[string]interface{}, ts int) (err error) {

	if rtmp.flv == nil {
		if err = rtmp.openFlv(false); err != nil {
			return
		}
	}

	//buf := new(bytes.Buffer)
	data := []interface{}{}
	data = append(data, "onMetaData")
	data = append(data, body)

	dat, err := amf.EncodeAmf0(data, true)
	//fmt.Printf("writeMetaData %v %#v\n", ts, dat)
	rdr := bytes.NewBuffer(dat)
	err = rtmp.flv.WriteMetaData(rdr, ts)
	return
}
func (rtmp *Rtmp) writeAudio(rdr *bytes.Buffer, ts int) (err error) {
	if rtmp.flv == nil {
		if err = rtmp.openFlv(false); err != nil {
			return
		}
	}
	err = rtmp.flv.WriteAudio(rdr, ts)
	return
}
func (rtmp *Rtmp) writeVideo(rdr *bytes.Buffer, ts int) (err error) {
	if rtmp.flv == nil {
		if err = rtmp.openFlv(false); err != nil {
			return
		}
	} /*else if (!rtmp.flv.VideoExists() && rtmp.flv.AudioExists()) && ts > 1000 {
		if err = rtmp.openFlv(true); err != nil {
			return
		}
	}*/
	err = rtmp.flv.WriteVideo(rdr, ts)
	return
}
func (rtmp *Rtmp) SetFixAggrTimestamp(sw bool) {
	rtmp.fixAggrTimestamp = sw
}
func (rtmp *Rtmp) CheckStatus(label string, ts int, data interface{}, waitPause bool) (done, incomplete, pauseFound bool, err error) {
	code, ok := obj.FindString(data, "code")
	if (! ok) {
		err = fmt.Errorf("%s: code Not found", label)
		return
	}

	switch code {
	case "NetStream.Pause.Notify":
		if waitPause {
			pauseFound = true
		}
	case "NetStream.Unpause.Notify":
	case "NetStream.Play.Stop":
	case "NetStream.Play.Complete":
		fmt.Printf("NetStream.Play.Complete: last timestamp: %d\n", rtmp.flv.GetLastTimestamp())
		if (ts + 1000) > rtmp.duration {
			done = true
		} else {
			incomplete = true
		}
	case "NetStream.Play.Start":
	case "NetStream.Play.Reset":
	default:
		fmt.Printf("[FIXME] Unknown Code: %s\n", code)
	}
	return
}
// trId: transaction id to find
func (rtmp *Rtmp) recvChunk(findTrId int, waitPause bool) (done, incomplete, trFound, pauseFound bool, err error) {
	rtmp.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	ts, msg_t, res, rdbytes, err := decodeOne(rtmp.conn, rtmp.chunkSizeRecv, rtmp.chunkInfo)
	if err != nil {
		if rdbytes == 0 {
			//[FIXME]下記が必要ならコメントを残す
			//err = nil
			return
		}
		return
	}

	// byte counter for acknowledgement
	rtmp.totalReadBytes += rdbytes
	rtmp.readCount += rdbytes
	if rtmp.readCount >= (rtmp.windowSize / 2) {
		rtmp.readCount = 0
		if err = rtmp.acknowledgement(); err != nil {
			return
		}
	}

	// debug log
	if true {
		if /*rtmp.isRecorded &&*/ rtmp.duration > 0 {
			switch msg_t {
				case TID_AUDIO, TID_VIDEO, TID_AGGREGATE:
					if ts >= rtmp.nextLogTs {
						fmt.Printf("#%8d/%d(%4.1f%%) : %2d %s\n", ts, rtmp.duration, float64(ts)/float64(rtmp.duration)*100, msg_t, rtmp.flvName)
						rtmp.nextLogTs = ts + 10000
					}
				//case TID_AMF0COMMAND, TID_AMF3COMMAND, TID_AMF0DATA, TID_AMF3DATA:
				default:
					//fmt.Printf("#%8d/%d(%4.1f%%) : %2d %#v\n", ts, rtmp.duration, float64(ts)/float64(rtmp.duration)*100, msg_t, res)
			}
		} else {
			switch msg_t {
				case TID_AUDIO, TID_VIDEO, TID_AGGREGATE:
					if ts >= rtmp.nextLogTs {
						fmt.Printf("#%8d : %s\n", ts, rtmp.flvName)
						rtmp.nextLogTs = ts + 10000
					}
				//case TID_AMF0COMMAND, TID_AMF3COMMAND, TID_AMF0DATA, TID_AMF3DATA:
					//fmt.Printf("#%8d : %2d\n", ts, msg_t)
				default:
					//fmt.Printf("#%8d : %2d %#v\n", ts, msg_t, res)
			}
		}
	}

	switch msg_t {
	case TID_AUDIO:
		if ts > rtmp.timestamp {
			rtmp.timestamp = ts
		}
		if err = rtmp.writeAudio(res.(*bytes.Buffer), ts); err != nil {
			return
		}

	case TID_VIDEO:
		if ts > rtmp.timestamp {
			rtmp.timestamp = ts
		}
		if err = rtmp.writeVideo(res.(*bytes.Buffer), ts); err != nil {
			return
		}

	case TID_AGGREGATE:
		if ts > rtmp.timestamp {
			rtmp.timestamp = ts
		}
		var fstTs int
		for i, v := range res.([]message) {
			var tsAggr int
			if rtmp.fixAggrTimestamp {
				var delta int
				if i == 0 {
					fstTs = v.timestamp
				}
				delta = v.timestamp - fstTs
				tsAggr = ts + delta
				//fmt.Printf("FixAggrTs: fixed(%d), delta(%d), ts(%d), mts(%d)\n", tsAggr, delta, ts, v.timestamp)
			} else {
				if i == 0 {
					if ts != v.timestamp {
						err = fmt.Errorf("aggregate timestamp incorrect: ts:(%v) vs aggr[0].ts(%v)", ts, v.timestamp)
						return
					}
				}
				tsAggr = v.timestamp
			}

			if /*rtmp.isRecorded &&*/ rtmp.duration > 0 {
				switch v.msg_t {
					case TID_AUDIO, TID_VIDEO:
						// fmt.Printf(" %8d/%d(%4.1f%%) : %2d\n", tsAggr, rtmp.duration, float64(tsAggr)/float64(rtmp.duration)*100, v.msg_t)
				}
			}

			switch v.msg_t {
			case TID_AUDIO:
				// audio
				if err = rtmp.writeAudio(v.data, tsAggr); err != nil {
					return
				}

			case TID_VIDEO:
				// video
				if err = rtmp.writeVideo(v.data, tsAggr); err != nil {
					return
				}
			}
		}

	case TID_AMF0DATA, TID_AMF3DATA:
		obj.PrintAsJson(res)
		list, ok := res.([]interface{})
		if (! ok) {
			err = fmt.Errorf("result AMF Data is not array")
			return
		}

		if len(list) >= 2 {
			name, ok := list[0].(string)
			if (! ok) {
				err = fmt.Errorf("result AMF Data[0] is not string")
				return
			}

			switch name {
			case "onPlayStatus":
				done, incomplete, pauseFound, err = rtmp.CheckStatus("onPlayStatus", ts, list[1], waitPause)

			case "onMetaData":
				dur, ok := obj.FindFloat64(list[1], "duration")
				if ok {
					rtmp.duration = int(dur * 1000)
				} else {
					if rtmp.isRecorded {
						fmt.Println("[WARN] onMetaData: duration not found")
					}
				}
				if meta, ok := list[1].(map[string]interface{}); ok {
					rtmp.writeMetaData(meta, ts)
				}

				_, ok = obj.FindVal(list[1], "videoframerate")
				if ok {
					rtmp.VideoExists = true
				}
			}
		}

	case TID_AMF0COMMAND, TID_AMF3COMMAND:
		obj.PrintAsJson(res)

		list, ok := res.([]interface{})
		if (! ok) {
			err = fmt.Errorf("result AMF Command is not array")
			return
		}

		if len(list) >= 3 {
			name, ok := list[0].(string)
			if (! ok) {
				err = fmt.Errorf("result AMF Command name is not string")
				return
			}
			trIdFloat, ok := list[1].(float64)
			if (! ok) {
				err = fmt.Errorf("result AMF Command transaction id is not number")
				return
			}
			trId := int(trIdFloat)
			if (trId > 0) && (trId == findTrId) {
				trFound = true
			}

			switch name {
			case "_error", "close":
				err = fmt.Errorf("AMF command not success: transaction id(%d) -> %s", trId, name)
				return
			case "onStatus":
				done, incomplete, pauseFound, err = rtmp.CheckStatus("onStatus", ts, list[3], waitPause)
			}
		}

	case TID_SETCHUNKSIZE:
		rtmp.chunkSizeRecv = res.(int)

	case TID_WINDOW_ACK_SIZE:
		rtmp.windowSize = res.(int)

	case TID_USERCONTROL:
		switch res.([]int)[0] {
			case UC_PINGREQUEST:
				//fmt.Printf("ping request %d\n", res.([]int)[1])
				if err = rtmp.pingResponse(res.([]int)[1]); err != nil {
					return
				}

			case UC_STREAMBEGIN:
				rtmp.streamId = res.([]int)[1]

			case UC_STREAMISRECORDED:
				fmt.Printf("stream is recorded\n")
				rtmp.isRecorded = true

			case UC_BUFFEREMPTY:
				if rtmp.isRecorded {
					fmt.Printf("required Seek: %d\n", rtmp.timestamp)
					ts := rtmp.timestamp - 10000
					if ts < 0 {
						ts = 0
					}
					done, incomplete, err = rtmp.PauseUnpause(ts)
					if done || incomplete || err != nil {
						return
					}
					//rtmp.Seek(ts)
				}
		}
	default:
		//fmt.Printf("got: %8d %d %#v\n", ts, msg_t, res)
	}
	return
}

func (rtmp *Rtmp) Close() (err error) {
	err = rtmp.conn.Close()
	return
}

func (rtmp *Rtmp) SetPeerBandwidth(wsz, lim int) (err error) {
	buff, err := encodeSetPeerBandwidth(wsz, lim)
	if err != nil {
		return
	}
	if _, err = buff.WriteTo(rtmp.conn); err != nil {
		return
	}
	return
}


func (rtmp *Rtmp) pingResponse(timestamp int) (err error) {
	buff, err := encodePingResponse(timestamp)
	if _, err = buff.WriteTo(rtmp.conn); err != nil {
		return
	}
	return
}
func (rtmp *Rtmp) acknowledgement() (err error) {
	buff, err := encodeAcknowledgement(rtmp.totalReadBytes)
	if _, err = buff.WriteTo(rtmp.conn); err != nil {
		return
	}
	return
}
func (rtmp *Rtmp) WindowAckSize(asz int) (err error) {
	buff, err := encodeWindowAckSize(asz)
	if _, err = buff.WriteTo(rtmp.conn); err != nil {
		return
	}
	return
}
func (rtmp *Rtmp) SetBufferLength(streamId, len int) (err error) {
	buff, err := encodeSetBufferLength(streamId, len)
	if _, err = buff.WriteTo(rtmp.conn); err != nil {
		return
	}
	return
}

// command name, transaction ID, and command object
func (rtmp *Rtmp) Command(name string, args []interface{}) (err error) {
	var trId int
	var csId int
	var streamId int
	switch name {
		case "connect":
			rtmp.transactionId = 1
			trId     = rtmp.transactionId
			csId     = 3
			streamId = 0

		case "play", "seek", "pause":
			trId     = 0
			csId     = 8
			streamId = 1

		default:
			// createStream, call, close, ...
			rtmp.transactionId++
			trId     = rtmp.transactionId
			csId     = 3
			streamId = 0
	}
	cmd := []interface{}{name, trId}
	cmd = append(cmd, args...)
obj.PrintAsJson(cmd)
	body, err := amf.EncodeAmf0(cmd, false)
	wbuff, err := amf0Command(rtmp.chunkSizeSend, csId, streamId, body)

	if _, err = wbuff.WriteTo(rtmp.conn); err != nil {
		return
	}

	if trId > 0 {
		if _, _, err = rtmp.waitCommand(trId); err != nil {
			return
		}
	}

	return
}

func (rtmp *Rtmp) Unpause(timestamp int) (err error) {
	var data []interface{}
	data = append(data, nil)
	data = append(data, false)
	data = append(data, timestamp)

	err = rtmp.Command("pause", data)

	return
}
func (rtmp *Rtmp) Pause(timestamp int) (err error) {
	var data []interface{}
	data = append(data, nil)
	data = append(data, true)
	data = append(data, timestamp)

	err = rtmp.Command("pause", data)

	return
}
func (rtmp *Rtmp) PauseUnpause(timestamp int) (done, incomplete bool, err error) {
	if err = rtmp.Pause(timestamp); err != nil {
		return
	}
fmt.Println("paused")
	done, incomplete, err = rtmp.WaitPause()
	if done || incomplete || err != nil {
		return
	}
fmt.Println("wait pause")
	if err = rtmp.Unpause(timestamp); err != nil {
		return
	}
fmt.Println("Unpaused")
	return
}
func (rtmp *Rtmp) PlayTime(stream string, timestamp int) (err error) {
	var data []interface{}
	data = append(data, nil)
	data = append(data, stream)

	data = append(data, timestamp) // Start
	// NicoOfficialTs, Never append Duration and flush
	if false {
		data = append(data, -1) // Duration
		data = append(data, true) // flush
	}

	err = rtmp.Command("play", data)

	return
}
func (rtmp *Rtmp) Play(stream string) error {
	return rtmp.PlayTime(stream, -5000)
}
func (rtmp *Rtmp) Seek(timestamp int) (err error) {
	//fmt.Printf("debug Seek to %d\n", timestamp)
	var data []interface{}
	data = append(data, nil)
	data = append(data, timestamp)

	err = rtmp.Command("seek", data)

	//fmt.Printf("debug Seek done\n")
	return
}
func (rtmp *Rtmp) CreateStream() (err error) {
	var data []interface{}
	data = append(data, nil)

	err = rtmp.Command("createStream", data)

	return
}

func handshake(conn *net.TCPConn) (err error) {

	wbuff := bytes.NewBuffer(nil)

	// C0
	wbuff.WriteByte(3)
	// C1
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	io.CopyN(wbuff, rnd, 1536)

	// Send C0+C1
	if _, err = wbuff.WriteTo(conn); err != nil {
		return
	}

	// Recv S0
	if _, err = io.CopyN(ioutil.Discard, conn, 1); err != nil {
		return
	}

	// Recv S1
	if _, err = io.CopyN(wbuff, conn, 1536); err != nil {
		return
	}

	// Send C2(=S1)
	if _, err = wbuff.WriteTo(conn); err != nil {
		return
	}
	// Recv S2
	if _, err = io.CopyN(ioutil.Discard, conn, 1536); err != nil {
		return
	}
	return
}



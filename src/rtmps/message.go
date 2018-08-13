package rtmps

import (
	"encoding/binary"
	"time"
	"bytes"
	"log"
	"io"

	"fmt"
	"../amf"
)

const (
	TID_SETCHUNKSIZE = 1
	TID_ABORT = 2
	TID_ACKNOWLEDGEMENT = 3
	TID_USERCONTROL = 4
	TID_WINDOW_ACK_SIZE = 5
	TID_SETPEERBANDWIDTH = 6
	TID_AUDIO = 8
	TID_VIDEO = 9
	TID_AMF3COMMAND = 17
	TID_AMF0COMMAND = 20
	TID_AMF0DATA = 18
	TID_AMF3DATA = 15
	TID_AGGREGATE = 22
)

const (
	UC_STREAMBEGIN = 0
	UC_STREAMEOF = 1
	UC_STREAMDRY = 2
	UC_SETBUFFERLENGTH = 3
	UC_STREAMISRECORDED = 4
	UC_PINGREQUEST = 6
	UC_PINGRESPONSE = 7

	UC_BUFFEREMPTY = 31
	UC_BUFFERREADY = 32
)


func intToBE16(num int) (data []byte) {
	tmp := make([]byte, 2)
	binary.BigEndian.PutUint16(tmp, uint16(num))
	data = append(data, tmp[:]...)
	return
}
func intToBE24(num int) (data []byte) {
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint32(tmp, uint32(num))
	data = append(data, tmp[1:]...)
	return
}
func intToBE32(num int) (data []byte) {
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint32(tmp, uint32(num))
	data = append(data, tmp[:]...)
	return
}
func intToLE32(num int) (data []byte) {
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(num))
	data = append(data, tmp[:]...)
	return
}


func chunkBasicHeader(fmt, csid int) (data []byte) {
	if 2 <= csid && csid <= 63 {
		b := byte(((fmt & 3) << 6) | (csid & 0x3F))
		data = append(data, b)

	} else if 64 <= csid && csid <= 319 {
		b0 := byte((fmt & 3) << 6)
		b1 := byte(csid - 64)
		data = append(data, b0, b1)

	} else if 320 <= csid && csid <= 65599 {
		b0 := byte(((fmt & 3) << 6) | 1)
		b1 := byte((csid & 0xFF) - 64)
		b2 := byte(csid >> 8)
		data = append(data, b0, b1, b2)

	} else {
		log.Printf("[FIXME] Chunk basic header: csid out of range: %d", csid)
	}

	return
}

var start = millisec()

func millisec() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
func getTime() int {
	delta := millisec() - start
	return int(delta)
}

func type0(buff *bytes.Buffer, csId int, typeId byte, streamId int, length int) {
	buff.Write(chunkBasicHeader(0, csId))

	// timestamp
	//buff.Write(intToBE24(getTime()))
	buff.Write(intToBE24(0))
	// message length
	buff.Write(intToBE24(length))
	// message type id
	buff.WriteByte(typeId)
	// Stream ID
	buff.Write(intToLE32(streamId))
	// body
	//buff.Write(body)

	return
}
func type3(buff *bytes.Buffer, csId int) {
	buff.Write(chunkBasicHeader(3, csId))
}
func encodeAcknowledgement(asz int) (buff *bytes.Buffer, err error){
	buff = bytes.NewBuffer(nil)
	bsz := intToBE32(asz)
	type0(buff, 2, TID_ACKNOWLEDGEMENT, 0, len(bsz))
	if _, err = buff.Write(bsz); err != nil {
		return
	}
	return
}
func encodeWindowAckSize(asz int) (buff *bytes.Buffer, err error){
	buff = bytes.NewBuffer(nil)
	bsz := intToBE32(asz)
	type0(buff, 2, TID_WINDOW_ACK_SIZE, 0, len(bsz))
	if _, err = buff.Write(bsz); err != nil {
		return
	}
	return
}
func encodeSetPeerBandwidth(wsz, lim int) (buff *bytes.Buffer, err error){
	buff = bytes.NewBuffer(nil)
	b := intToBE32(wsz)
	b = append(b, byte(lim))
	type0(buff, 2, TID_SETPEERBANDWIDTH, 0, len(b))
	if _, err = buff.Write(b); err != nil {
		return
	}
	return
}

func encodePingResponse(timestamp int) (buff *bytes.Buffer, err error){
	buff = bytes.NewBuffer(nil)

	var body []byte
	body = append(body, intToBE16(UC_PINGRESPONSE)...)
	body = append(body, intToBE32(timestamp)...)

	type0(buff, 2, TID_USERCONTROL, 0, len(body))
	if _, err = buff.Write(body); err != nil {
		return
	}
	return
}
func encodeSetBufferLength(streamId, length int) (buff *bytes.Buffer, err error){
	buff = bytes.NewBuffer(nil)

	var body []byte
	body = append(body, intToBE16(UC_SETBUFFERLENGTH)...)
	body = append(body, intToBE32(streamId)...)
	body = append(body, intToBE32(int(length))...)

	type0(buff, 2, TID_USERCONTROL, 0, len(body))
	if _, err = buff.Write(body); err != nil {
		return
	}
	return
}
func amf0Command(chunkSize, csId, streamId int, body []byte) (wbuff *bytes.Buffer, err error) {
	wbuff = bytes.NewBuffer(nil)
	rbuff := bytes.NewBuffer(body)

	type0(wbuff, csId, TID_AMF0COMMAND, streamId, rbuff.Len())
	if chunkSize < rbuff.Len() {
		if _, err = io.CopyN(wbuff, rbuff, int64(chunkSize)); err != nil {
			return
		}
	} else {
		if _, err = io.CopyN(wbuff, rbuff, int64(rbuff.Len())); err != nil {
			return
		}
	}

	for rbuff.Len() > 0 {
		type3(wbuff, csId)

		if chunkSize < rbuff.Len() {
			if _, err = io.CopyN(wbuff, rbuff, int64(chunkSize)); err != nil {
				return
			}
		} else {
			if _, err = io.CopyN(wbuff, rbuff, int64(rbuff.Len())); err != nil {
				return
			}
		}
	}

	//log.Fatalf("amf0Command %#v", wbuff)

	return
}



func decodeFmtCsId(rdr io.Reader, msg *rtmpMsg) (err error) {
	b0 := make([]byte, 1)
	msg.hdrLength++
	_, err = io.ReadFull(rdr, b0); if err != nil {
		return
	}
	format := (int(b0[0]) >> 6) & 3
	csId := int(b0[0]) & 0x3F
	switch csId {
		case 0:
			b1 := make([]byte, 1)
			msg.hdrLength++
			if _, err = io.ReadFull(rdr, b1); err != nil {
				return
			}
			csId = int(b1[0]) + 64

		case 1:
			b1 := make([]byte, 2)
			msg.hdrLength += 2
			if _, err = io.ReadFull(rdr, b1); err != nil {
				return
			}
			csId = (int(b1[1]) << 8) | (int(b1[0]) + 64)
	}

	msg.format = format
	msg.csId = csId
	if (! msg.readingBody) {
		msg.formatOrigin = format
		msg.csIdOrigin = csId
	}
	// fmt.Printf("debug format type %v csid %v\n", format, csId)
	return
}

func decodeInt8(rdr io.Reader) (num int, err error) {
	buf := make([]byte, 1)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	num = int(buf[0])
	return
}
func decodeBEInt16(rdr io.Reader) (num int, err error) {
	buf := make([]byte, 2)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	num = (int(buf[0]) << 8) | int(buf[1])
	return
}
func decodeBEInt24(rdr io.Reader) (num int, err error) {
	buf := make([]byte, 3)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	num = (int(buf[0]) << 16) | (int(buf[1]) << 8) | int(buf[2])
	return
}
func decodeBEInt32(rdr io.Reader) (num int, err error) {
	buf := make([]byte, 4)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	num = (int(buf[0]) << 24) | (int(buf[1]) << 16) | (int(buf[2]) << 8) | int(buf[3])
	return
}
func decodeLEInt32(rdr io.Reader) (num int, err error) {
	buf := make([]byte, 4)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	num = (int(buf[3]) << 24) | (int(buf[2]) << 16) | (int(buf[1]) << 8) | int(buf[0])
	return
}

func decodeTimestamp(rdr io.Reader, msg *rtmpMsg) (err error) {
	msg.hdrLength += 3
	timestamp, err := decodeBEInt24(rdr)
	if err != nil {
		return
	}
	msg.timestampField = timestamp

	return
}
func decodeTimestampEX(rdr io.Reader, msg *rtmpMsg) (err error) {
	msg.hdrLength += 4
	timestamp, err := decodeBEInt32(rdr)
	if err != nil {
		return
	}
	//fmt.Printf("decodeTimestampEX %v\n", timestamp)
	if (! msg.readingBody) {
		msg.timestampEx = timestamp
	}

	return
}
func decodeMsgLength(rdr io.Reader, msg *rtmpMsg) (err error) {
	msg.hdrLength += 3
	length, err := decodeBEInt24(rdr)
	msg.msgLength = length
	return
}
func decodeMsgType(rdr io.Reader, msg *rtmpMsg) (err error) {
	msg.hdrLength += 1
	msg_t, err := decodeInt8(rdr)
	msg.msgTypeId = msg_t
	return
}
func decodeStreamId(rdr io.Reader, msg *rtmpMsg) (err error) {
	msg.hdrLength += 4
	sid, err := decodeLEInt32(rdr)
	msg.msgStreamId = sid
	return
}

func decodeType0(rdr io.Reader, msg *rtmpMsg) (err error) {
	if err = decodeTimestamp(rdr, msg); err != nil {
		return
	}
	if err = decodeMsgLength(rdr, msg); err != nil {
		return
	}
	if err = decodeMsgType(rdr, msg); err != nil {
		return
	}
	err = decodeStreamId(rdr, msg)
	return
}
func decodeType1(rdr io.Reader, msg *rtmpMsg) (err error) {
	if err = decodeTimestamp(rdr, msg); err != nil {
		return
	}
	if err = decodeMsgLength(rdr, msg); err != nil {
		return
	}
	err = decodeMsgType(rdr, msg)
	return
}
func decodeType2(rdr io.Reader, msg *rtmpMsg) (err error) {
	err = decodeTimestamp(rdr, msg)
	return
}




type rtmpMsg struct {
	format int
	formatOrigin int
	csId int
	csIdOrigin int
	timestampField int
	timestampDelta int
	timestampEx int
	timestampActual int
	msgLength int
	msgTypeId int
	msgStreamId int
	bodyBuff *bytes.Buffer

	readingBody bool
	hdrLength int
	splitCount int
}

func readChunkBody(rdr io.Reader, msg *rtmpMsg, csz int) (err error) {

	if msg.bodyBuff == nil {
		msg.bodyBuff = bytes.NewBuffer(nil)
	}
	rem := msg.msgLength - msg.bodyBuff.Len()
//fmt.Printf("readChunkBody: %v %v\n", msg.msgLength, msg.bodyBuff.Len())
	if rem > csz {
		_, err = io.CopyN(msg.bodyBuff, rdr, int64(csz))
	} else {
		_, err = io.CopyN(msg.bodyBuff, rdr, int64(rem))
	}
	if err != nil {
		return
	}

	return
}

func decodeHeader(rdr io.Reader, msg *rtmpMsg) (err error) {
	if err = decodeFmtCsId(rdr, msg); err != nil {
		return
	}
	switch msg.format {
		case 0:
			if err = decodeType0(rdr, msg); err != nil {
				return
			}
		case 1:
			if err = decodeType1(rdr, msg); err != nil {
				return
			}
		case 2:
			if err = decodeType2(rdr, msg); err != nil {
				return
			}
		case 3:
			if (msg.readingBody) {
				msg.splitCount++
				if msg.csId != msg.csIdOrigin {
					err = &DecodeError{
						Fun: "decodeHeader",
						Msg: fmt.Sprintf("msg.csId(%d) != msg.csIdOrigin(%d)", msg.csId, msg.csIdOrigin),
					}
					return
				}
			}
		default:
			err = &DecodeError{
				Fun: "decodeHeader",
				Msg: fmt.Sprintf("Unknown fmt: %v", msg.format),
			}
			return
	}

	return
}

func decodeSetChunkSize(rbuff *bytes.Buffer) (csz int, err error) {
	num, e := decodeBEInt32(rbuff)
	if e != nil {
		err = e
		return
	}
	csz = num & 0x7fffffff
	return
}

func decodeWindowAckSize(rbuff *bytes.Buffer) (asz int, err error) {
	asz, e := decodeBEInt32(rbuff)
	if e != nil {
		err = e
		return
	}
	return
}

func decodeSetPeerBandwidth(rbuff *bytes.Buffer) (res []int, err error) {
	wsz, err := decodeBEInt32(rbuff)
	if err != nil {
		return
	}
	lim, err := decodeInt8(rbuff)
	if err != nil {
		return
	}
	res = append(res, wsz, lim)
	return
}
func decodeUserControl(rbuff *bytes.Buffer) (res []int, err error) {
	evt, err := decodeBEInt16(rbuff)
	if err != nil {
		return
	}
	res = append(res, evt)
	switch evt {
		case UC_BUFFEREMPTY, UC_BUFFERREADY: // Buffer Empty, Buffer Ready
			// http://repo.or.cz/w/rtmpdump.git/blob/8880d1456b282ee79979adbe7b6a6eb8ad371081:/librtmp/rtmp.c#l2787

		case
			UC_STREAMBEGIN,
			UC_STREAMEOF,
			UC_STREAMDRY,
			UC_STREAMISRECORDED,
			UC_PINGREQUEST,
			UC_PINGRESPONSE:
			// 4-byte stream id
			num, e := decodeBEInt32(rbuff)
			if e != nil {
				err = e
				return
			}
			res = append(res, num)

		case UC_SETBUFFERLENGTH:
			// 4-byte stream id
			sid, e := decodeBEInt32(rbuff)
			if e != nil {
				err = e
				return
			}
			res = append(res, sid)
			// 4-byte buffer length
			bsz, e := decodeBEInt32(rbuff)
			if e != nil {
				err = e
				return
			}
			res = append(res, bsz)

		default:
			err = &DecodeError{
				Fun: "decodeUserControl",
				Msg: fmt.Sprintf("Unknown User control: %v", evt),
			}
			return
	}
	return
}

type message struct {
	msg_t int
	timestamp int
	data *bytes.Buffer
}
func decodeMessage(rbuff *bytes.Buffer) (res message, err error) {
	msg_t, err := decodeInt8(rbuff)
	if err != nil {
		return
	}
	plen, err := decodeBEInt24(rbuff)
	if err != nil {
		return
	}
	ts_0, err := decodeBEInt24(rbuff)
	if err != nil {
		return
	}
	ts_1, err := decodeInt8(rbuff)
	if err != nil {
		return
	}
	ts := (ts_1 << 24) | ts_0

	// stream id
	_, err = decodeBEInt24(rbuff)
	if err != nil {
		return
	}
//fmt.Printf("debug decodeMessage: type(%v) len(%v) ts(%v)\n", msg_t, plen, ts_0)
	buff := bytes.NewBuffer(nil)
	if _, err = io.CopyN(buff, rbuff, int64(plen)); err != nil {
		return
	}

	// backPointer
	_, err = decodeBEInt32(rbuff)
	if err != nil {
		return
	}

	res = message{
		msg_t: msg_t,
		timestamp: ts,
		data: buff,
	}

	return
}
func decodeAggregate(rbuff *bytes.Buffer) (res []message, err error) {
	for rbuff.Len() > 0 {
		msg, e := decodeMessage(rbuff)
		if e != nil {
			err = e
			return
		}
		res = append(res, msg)
	}
	return
}

func decodeOne(rdr io.Reader, csz int, info map[int] chunkInfo) (ts int, msg_t int, res interface{}, rsz int, err error) {
	msg := rtmpMsg{}

	// rtmp header
	if err = decodeHeader(rdr, &msg); err != nil {
		return
	}

	// restore fields from previous chunk header

	var prevChunk chunkInfo
	if msg.formatOrigin != 0 {
		var ok bool
		if prevChunk, ok = info[msg.csIdOrigin]; (! ok) {
			err = &DecodeError{
				Fun: "decodeOne",
				Msg: fmt.Sprintf("Not exists previous chunk(csId = %v)", msg.csIdOrigin),
			}
			return
		}
	}
	//fmt.Printf("debug decodeOne msg.timestampField %d\n", msg.timestampField)
	if (msg.timestampField == 0xffffff) || ((msg.formatOrigin == 3) && (prevChunk.timestampField == 0xffffff)) {
		if err = decodeTimestampEX(rdr, &msg); err != nil {
			return
		}
		//fmt.Printf("%#v\n", msg)
		switch msg.formatOrigin {
			case 0:
				msg.timestampActual = msg.timestampEx
				msg.timestampDelta = msg.timestampEx
			case 1, 2:
				msg.timestampActual = prevChunk.timestampActual + msg.timestampEx
				msg.timestampDelta = msg.timestampEx
			case 3:
				msg.timestampActual = msg.timestampEx
				msg.timestampDelta = msg.timestampEx
				msg.timestampField = 0xffffff
		}
	} else {
		switch msg.formatOrigin {
			case 0:
				msg.timestampActual = msg.timestampField
				msg.timestampDelta = msg.timestampField
			case 1, 2:
				msg.timestampActual = prevChunk.timestampActual + msg.timestampField
				msg.timestampDelta = msg.timestampField
			case 3:
				msg.timestampActual = prevChunk.timestampActual + prevChunk.timestampDelta
				msg.timestampDelta = prevChunk.timestampDelta
		}
	}

	switch msg.formatOrigin {
		case 1:
			msg.msgStreamId = prevChunk.msgStreamId
		case 2, 3:
			msg.msgLength = prevChunk.msgLength
			msg.msgTypeId = prevChunk.msgTypeId
			msg.msgStreamId = prevChunk.msgStreamId
	}

	info[msg.csId] = chunkInfo{
		timestampField: msg.timestampField,
		timestampDelta: msg.timestampDelta,
		timestampActual: msg.timestampActual,
		msgLength: msg.msgLength,
		msgTypeId: msg.msgTypeId,
		msgStreamId: msg.msgStreamId,
	}

	ts = msg.timestampActual

	msg.readingBody = true

	// rtmp payload
	for {
		if err = readChunkBody(rdr, &msg, csz); err != nil {
			return
		}

		if msg.msgLength <= msg.bodyBuff.Len() {
			break
		}

		//if err = decodeFmtCsId(rdr, &msg); err != nil {
		if err = decodeHeader(rdr, &msg); err != nil {
			return
		}

		// timestamp extended
		if (msg.timestampField == 0xffffff) {
			if err = decodeTimestampEX(rdr, &msg); err != nil {
				return
			}
		}
	}

//fmt.Printf("debug rtmp decodeOne: %#v\n", msg)
	// read byte count
	rsz = msg.hdrLength + msg.msgLength

	msg_t = msg.msgTypeId
	switch msg.msgTypeId {
		case TID_AGGREGATE:
			if res, err = decodeAggregate(msg.bodyBuff); err != nil {
				return
			}

		case TID_AUDIO, TID_VIDEO:
			res = msg.bodyBuff

		case TID_WINDOW_ACK_SIZE:
			if res, err = decodeWindowAckSize(msg.bodyBuff); err != nil {
				return
			}
		case TID_SETPEERBANDWIDTH:
			if res, err = decodeSetPeerBandwidth(msg.bodyBuff); err != nil {
				return
			}
		case TID_AMF0COMMAND:
			if res, err = amf.DecodeAmf0(msg.bodyBuff.Bytes()); err != nil {
				return
			}
		case TID_AMF3COMMAND:
			if res, err = amf.DecodeAmf0(msg.bodyBuff.Bytes(), true); err != nil {
				return
			}
		case TID_AMF0DATA:
			if res, err = amf.DecodeAmf0(msg.bodyBuff.Bytes()); err != nil {
				return
			}
		case TID_SETCHUNKSIZE:
			if res, err = decodeSetChunkSize(msg.bodyBuff); err != nil {
				return
			}
		case TID_USERCONTROL:
			if res, err = decodeUserControl(msg.bodyBuff); err != nil {
				return
			}
		default:
			err = &DecodeError{
				Fun: "decodeOne",
				Msg: fmt.Sprintf("msgTypeId: not implement: %v\n%#v", msg.msgTypeId, msg.bodyBuff.Bytes()),
			}
			return
	}

	return
}
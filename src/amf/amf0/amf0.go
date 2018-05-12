package amf0

import (
	"bytes"
	"io"
	"encoding/binary"
	"math"
	"fmt"
	"log"
	"../amf3"
	"../amf_t"
)

func encodeNumber(num float64, buff *bytes.Buffer) (err error) {
	if err = buff.WriteByte(0); err != nil {
		return
	}

	bits := math.Float64bits(num)
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, bits)
	if _, err = buff.Write(bytes); err != nil {
		return
	}
	return
}
func encodeBoolean(b bool, buff *bytes.Buffer) (err error) {
	if err = buff.WriteByte(1); err != nil {
		return
	}

	var val byte
	if b {
		val = 1
	}

	if err = buff.WriteByte(val); err != nil {
		return
	}

	return
}
func encodeUtf8(s string, buff *bytes.Buffer) (err error) {
	bs := []byte(s)
	if len(bs) > 0xffff {
		err = fmt.Errorf("string too large")
		return
	}

	b0 := make([]byte, 2)
	binary.BigEndian.PutUint16(b0, uint16(len(bs)))
	if _, err = buff.Write(b0); err != nil {
		return
	}
	if _, err = buff.Write(bs); err != nil {
		return
	}
	return
}
func encodeString(s string, buff *bytes.Buffer) (err error) {
	if err = buff.WriteByte(2); err != nil {
		return
	}
	err = encodeUtf8(s, buff)
	return
}
func encodeObject(obj map[string]interface {}, buff *bytes.Buffer) (err error) {
	if err = buff.WriteByte(3); err != nil {
		return
	}

	for k, v := range obj {
		if err = encodeUtf8(k, buff); err != nil {
			return
		}
		if _, err = encode(v, false, buff); err != nil {
			return
		}
	}
	if _, err = buff.Write([]byte{0, 0, 9}); err != nil {
		return
	}
	return
}

func encodeNull(buff *bytes.Buffer) error {
	return buff.WriteByte(5)
}
func encodeSwitchToAmf3(buff *bytes.Buffer) error {
	return buff.WriteByte(0x11)
}
func encodeEcmaArray(data map[string]interface {}, buff *bytes.Buffer) (err error) {
	if err = buff.WriteByte(8); err != nil {
		return
	}
	buf4 := make([]byte, 4)
	binary.BigEndian.PutUint32(buf4, uint32(len(data)))
	if _, err = buff.Write(buf4); err != nil {
		return
	}

	for k, v := range data {
		if err = encodeUtf8(k, buff); err != nil {
			return
		}
		if _, err = encode(v, true, buff); err != nil {
			return
		}
	}
	if _, err = buff.Write([]byte{0, 0, 9}); err != nil {
		return
	}

	return
}
func encode(data interface{}, asEcmaArray bool, buff *bytes.Buffer) (toAmf3 bool, err error) {
	switch data.(type) {
		case string:
			err = encodeString(data.(string), buff)
		case float64:
			err = encodeNumber(data.(float64), buff)
		case int:
			err = encodeNumber(float64(data.(int)), buff)
		case bool:
			err = encodeBoolean(data.(bool), buff)
		case map[string]interface{}:
			if asEcmaArray {
				err = encodeEcmaArray(data.(map[string]interface{}), buff)
			} else {
				err = encodeObject(data.(map[string]interface{}), buff)
			}
		case []interface {}:
			m := make(map[string]interface{})
			for i, d := range data.([]interface {}) {
				k := fmt.Sprintf("%d", i)
				m[k] = d
			}
			err = encodeEcmaArray(m, buff)
		case nil:
			err = encodeNull(buff)
		case amf_t.SwitchToAmf3:
			toAmf3 = true
			err = encodeSwitchToAmf3(buff)
		default:
			log.Fatalf("amf0/encode %#v", data)
	}
	return
}

func Encode(data []interface{}, asEcmaArray bool) (b []byte, err error) {
	buff := bytes.NewBuffer(nil)
	for i, d := range data {
		var toAmf3 bool
		if toAmf3, err = encode(d, asEcmaArray, buff); err != nil {
			return
		}
		if toAmf3 {
			b2, e := amf3.Encode(data[i+1:])
			if e != nil {
				err = e
				return
			}
			b = append(b, buff.Bytes()...)
			b = append(b, b2...)
			return
		}
	}
	b = buff.Bytes()
	return
}

type objectEnd struct {}

func decodeString(rdr *bytes.Reader) (str string, err error) {
	buf := make([]byte, 2)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	len := (int(buf[0]) << 8) | int(buf[1])
	if len > 0 {
		buf := make([]byte, len)
		if _, err = io.ReadFull(rdr, buf); err != nil {
			return
		}
		str = string(buf)
	}
	return
}
func decodeNumber(rdr *bytes.Reader) (res float64, err error) {
	buf := make([]byte, 8)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}

	u64 := binary.BigEndian.Uint64(buf)
	res = math.Float64frombits(u64)
	return
}
func decodeBoolean(rdr *bytes.Reader) (res bool, err error) {
	buf := make([]byte, 1)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	if buf[0] == 0 {
		res = false
	} else {
		res = true
	}
	return
}
func decodeObject(rdr *bytes.Reader) (res map[string]interface{}, err error) {
	res = make(map[string]interface{})
	for {
		key, e := decodeString(rdr)
		if e != nil {
			err = e
			return
		}

		val, e := decodeOne(rdr)
		if e != nil {
			err = e
			return
		}
		if key == "" {
			switch val.(type) {
				case objectEnd:
					return
				default:
					log.Fatalf("decodeObject: parse error; Not object-end, %+s", val)
			}
		}
		res[key] = val
	}
	return
}
func decodeEcmaArray(rdr *bytes.Reader) (res map[string]interface{}, err error) {
	buf := make([]byte, 4)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	//count := binary.BigEndian.Uint32(buf)
	//log.Printf("decodeEcmaArray: Count: %v", count)
	res, err = decodeObject(rdr)

	return
}
func decodeStrictArray(rdr *bytes.Reader) (res []interface{}, err error) {
	buf := make([]byte, 4)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	count := binary.BigEndian.Uint32(buf)
	for i := uint32(0); i < count; i++ {
		re, e := decodeOne(rdr)
		if e != nil {
			err = e
			return
		}
		res = append(res, re)
	}
	return
}


func decodeOne(rdr *bytes.Reader) (res interface{}, err error) {
	buf := make([]byte, 1)
	if _, err = io.ReadFull(rdr, buf); err != nil {
		return
	}
	switch buf[0] {
		case 0: // Number
			res, err = decodeNumber(rdr)
		case 1: // Boolean
			res, err = decodeBoolean(rdr)
		case 2: // String
			res, err = decodeString(rdr)
		case 3:
			res, err = decodeObject(rdr)
		case 5: // Null
			res = nil
		case 6: // undefined
			res = nil
		case 8: // ECMA Array
			res, err = decodeEcmaArray(rdr)

		case 9: // Object End
			res = objectEnd{}
		case 10:
			res, err = decodeStrictArray(rdr)
		case 0x11: // Switch to AMF3
			dat, e := amf3.DecodeAll(rdr)
			if e != nil {
				err = e
				return
			}
			res = amf_t.AMF3{Data: dat}
		default:
			err = fmt.Errorf("Not implemented: type=%d", buf[0])
	}
	return
}



func DecodeAll(rdr *bytes.Reader) (res []interface{}, err error) {
	for rdr.Len() > 0 {
		re, e := decodeOne(rdr)
		if e != nil {
			err = e
			return
		}
		switch re.(type) {
			case amf_t.AMF3:
				res = append(res, re.(amf_t.AMF3).Data...)
			default:
				res = append(res, re)
		}
	}
	return
}

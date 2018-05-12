package amf3

import (
	"bytes"
	"io"
	"log"
	"fmt"
)

func decodeU29(rdr *bytes.Reader) (res int, err error) {
	for i := 0; i < 4; i++ {
		var num byte
		if num, err = rdr.ReadByte(); err != nil {
			return
		}
		var flg bool
		var val uint8
		if i == 3 {
			val = num
		} else {
			flg = (num & 0x80) == 0x80
			val = (num & 0x7f)
		}

		switch i {
			case 0:
				res = int(val)
			case 3:
				res = (res << 8) | int(val)
			default:
				res = (res << 7) | int(val)
		}
		if (! flg) {
			break
		}
	}
	return
}

// UTF-8-vr
func decodeString(rdr *bytes.Reader) (str string, err error) {
	// UTF-8-vr = U29S-ref
	// UTF-8-vr = U29S-value *(UTF8-char)
	u29, err := decodeU29(rdr)
	if err != nil {
		return
	}
	flag := (u29 & 1) != 0
	len := u29 >> 1

	if (! flag) {
		// string reference table index
		log.Fatalf("[FIXME] not implemented: UTF-8-vr = U29S-ref")
	} else {
		buf := make([]byte, len)
		if _, err = io.ReadFull(rdr, buf); err != nil {
			return
		}
		str = string(buf)
	}
	return
}

func assocOrUtf8Empty(rdr *bytes.Reader) (key string, val interface{}, err error) {
	key, err = decodeString(rdr)
	if err != nil {
		return
	}
	if key == "" {
		//fmt.Printf("assocOrUtf8Empty: string is empty\n")
		return
	}
	val, err = decodeOne(rdr)
	if err != nil {
		return
	}
	//log.Fatalf("assocOrUtf8Empty: key=%v, val=%v", key, val)
	return
}

func decodeOne(rdr *bytes.Reader) (res interface{}, err error) {
	format, err := rdr.ReadByte()
	if err != nil {
		return
	}
	switch format {
		case 6: // string-marker
			res, err = decodeString(rdr)
			if err != nil {
				return
			}
		case 9: // array-marker
		// array-marker U29O-ref
		// # array-marker U29A-value (UTF-8-empty | * (assoc-value) UTF-8-empty) * (value-type)
		// array-marker U29A-value * (assoc-value) UTF-8-empty * (value-type)
		// array-marker U29A-value UTF-8-empty * (value-type)
			u29, _ := decodeU29(rdr)
			flag := u29 & 1 != 0
			count := u29 >> 1
			if (! flag) {
				log.Fatalf("[FIXME] not implemented: array-type = array-marker U29O-ref")
			}
			if count == 0 { // [FIXME] condition OK?
				// associative, terminated by empty string
				assoc := make(map[string]interface{})
				for {
					k, v, e := assocOrUtf8Empty(rdr)
					if e != nil {
						//fmt.Printf("## amf3 associative: %+v\n", e)
						err = e
						return
					}
					if k == "" {
						break
					}
					assoc[k] = v
					//log.Printf("AMF3 array: %v = %v", k, v)
				}
				res = assoc
			}
			//log.Fatalf("AMF3 array: len: %d", count)
		default:
		log.Printf("%v\n", res)
		log.Fatalf("Not implemented: %d", format)
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
		res = append(res, re)
	}
	return
}


func encodeU29(num int, buff *bytes.Buffer) (err error) {
	if (0 <= num && num <= 0x7f) {
		if err = buff.WriteByte( byte(num & 0x7f) ); err != nil {
			return
		}
	} else if (0x80 <= num && num <= 0x3fff) {
		if err = buff.WriteByte( byte(0x80 | ((num >> 7) & 0x7f)) ); err != nil {
			return
		}
		if err = buff.WriteByte( byte(num & 0x7f) ); err != nil {
			return
		}
	} else if (0x4000 <= num && num <= 0x1fffff) {
		if err = buff.WriteByte( byte(0x80 | ((num >> 14) & 0x7f)) ); err != nil {
			return
		}
		if err = buff.WriteByte( byte(0x80 | ((num >> 7) & 0x7f)) ); err != nil {
			return
		}
		if err = buff.WriteByte( byte(num & 0x7f) ); err != nil {
			return
		}
	} else if (0x200000 <= num && num <= 0x3fffffff) {
		if err = buff.WriteByte( byte(0x80 | ((num >> 22) & 0x7f)) ); err != nil {
			return
		}
		if err = buff.WriteByte( byte(0x80 | ((num >> 15) & 0x7f)) ); err != nil {
			return
		}
		if err = buff.WriteByte( byte(0x80 | ((num >> 7) & 0x7f)) ); err != nil {
			return
		}
		if err = buff.WriteByte( byte(num & 0xff) ); err != nil {
			return
		}
	} else {
		err = fmt.Errorf("u29 overflow")
	}
	return
}

func encodeU28Flag(num int, flag bool, buff *bytes.Buffer) (err error) {
	if flag {
		err = encodeU29(((num << 1) | 1), buff)
	} else {
		err = encodeU29((num << 1), buff)
	}
	return
}


func encodeArray(data []interface {}, buff *bytes.Buffer) (err error) {
	// array-marker
	if err = buff.WriteByte(9); err != nil {
		return
	}
	// U29A-value; count of the dense portin of the Array
	if err = encodeU28Flag(len(data), true, buff); err != nil {
		return
	}
	// UTF-8-empty
	if err = buff.WriteByte(1); err != nil {
		return
	}
	for _, v := range data {
		if err = encode(v, buff); err != nil {
			return
		}
	}
	return
}

func encodeStringArray(data []string, buff *bytes.Buffer) error {
	var list []interface{}
	for _, v := range data {
		list = append(list, v)
	}
	return encodeArray(list, buff)
}
func encodeString(data string, buff *bytes.Buffer) (err error) {
	if err = buff.WriteByte(6); err != nil {
		return
	}
	bstr := []byte(data)
	// U29S-value
	if err = encodeU28Flag(len(bstr), true, buff); err != nil {
		return
	}
	if _, err = buff.Write(bstr); err != nil {
		return
	}
	return
}

func encode(data interface{}, buff *bytes.Buffer) (err error) {
	switch data.(type) {
		case string:
			err = encodeString(data.(string), buff)
		case []string:
			err = encodeStringArray(data.([]string), buff)
		default:
			log.Fatalf("amf0/encode %#v", data)
	}
	return
}
func Encode(data []interface{}) (b []byte, err error) {
	buff := bytes.NewBuffer(nil)
	for _, data := range data {
		if err = encode(data, buff); err != nil {
			return
		}
	}
	b = buff.Bytes()
	return
}
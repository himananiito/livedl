package flvs

import (
	"os"
	"fmt"
	"io"
	"encoding/binary"
	"bytes"
	"bufio"
)

type Flv struct {
	filename string
	file *os.File
	writer *bufio.Writer
	startAt int
	audioTimestamp int
	videoTimestamp int
}
func (flv *Flv) Flush() {
	if flv.writer != nil {
		flv.writer.Flush()
	}
}
func (flv *Flv) Close() {
	flv.Flush()
	if flv.file != nil {
		flv.file.Close()
	}
}
func Open(name string) (flv *Flv, err error) {
	file, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0777)
	if err != nil {
		return
	}

	flv = &Flv {
		filename: name,
		file: file,
		audioTimestamp: -1,
		videoTimestamp: -1,
	}

	stat, err := file.Stat()
	if err != nil {

	}

	// FLV header
	sz := stat.Size()
	if sz == 0 {
		if err = flv.writeHeader(); err != nil {
			flv.Close()
			return
		}
	}

	if err = flv.testHeader(); err != nil {
		flv.Close()
		return
	}


	flv.lastPacketTimestamp()

	if _, err = flv.file.Seek(0, 2); err != nil {
		return
	}
	ts := flv.GetLastTimestamp()
	if ts != 0 {
		fmt.Printf("[info] Seek point: %d\n", ts)
	}


	flv.writer = bufio.NewWriterSize(file, 256*1024)


	return
}
func (flv *Flv) AudioExists() bool {
	return flv.audioTimestamp >= 0
}
func (flv *Flv) VideoExists() bool {
	return flv.videoTimestamp >= 0
}
func (flv *Flv) testHeader() (err error) {
	if _, err = flv.file.Seek(0, 0); err != nil {
		return
	}

	b := make([]byte, 9)
	_, err = io.ReadFull(flv.file, b); if err != nil {
		return
	}
	if "FLV" != string(b[0:3]) {
		err = fmt.Errorf("magic number is not FLV")
		return
	}
	offset := binary.BigEndian.Uint32(b[5:9])
	flv.startAt = int(offset)

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

func (flv *Flv) writePacket(tag byte, rdr *bytes.Buffer, ts int) (err error) {
	buff := bytes.NewBuffer(nil)

	dataSize := intToBE24(rdr.Len())
	tagSize := intToBE32(11 + rdr.Len())

	// TagType
	if err = buff.WriteByte(tag); err != nil {
		return
	}
	// DataSize
	if _, err = buff.Write(dataSize); err != nil {
		return
	}

	// Timestamp
	tsBytes := intToBE32(ts)
	if _, err = buff.Write(tsBytes[1:4]); err != nil {
		return
	}
	// (TimestampExtended)
	if err = buff.WriteByte(tsBytes[0]); err != nil {
		return
	}
	// StreamID
	if _, err = buff.Write([]byte{0, 0, 0}); err != nil {
		return
	}

	// header
	if _, err = io.Copy(flv.writer, buff); err != nil {
		return
	}
	// data
	if _, err = io.Copy(flv.writer, rdr); err != nil {
		return
	}

	// PreviousTagSize
	if _, err = flv.writer.Write(tagSize); err != nil {
		return
	}

	return
}

func (flv *Flv) WriteAudio(rdr *bytes.Buffer, ts int) (err error) {
	if ts > flv.audioTimestamp {
		flv.audioTimestamp = ts
		err = flv.writePacket(8, rdr, ts)
	}
	return
}
func (flv *Flv) WriteVideo(rdr *bytes.Buffer, ts int) (err error) {
	if ts > flv.videoTimestamp {
		flv.videoTimestamp = ts
		err = flv.writePacket(9, rdr, ts)
	}
	return
}
func (flv *Flv) WriteMetaData(rdr *bytes.Buffer, ts int) (err error) {
	err = flv.writePacket(18, rdr, ts)
	return
}

func (flv *Flv) GetLastTimestamp() int {
	var min int
	if flv.audioTimestamp > flv.videoTimestamp {
		min = flv.videoTimestamp
	} else {
		min = flv.audioTimestamp
	}
	if min < 0 {
		return 0
	}
	return min
}

func (flv *Flv) lastPacketTimestamp() (err error) {
	defer flv.file.Seek(0, 2)

	if _, err = flv.file.Seek(-4, 2); err != nil {
		fmt.Printf("flv.lastPacketTimestamp: %#v\n", err)
		return
	}

	b0 := make([]byte, 4)
	b1 := make([]byte, 11)

	var audioFound bool
	var videoFound bool
	for !(audioFound && videoFound) {
		_, err = io.ReadFull(flv.file, b0); if err != nil {
			return
		}
		size := binary.BigEndian.Uint32(b0)
		//fmt.Printf("size: %d\n", size)
		if size == 0 {
			break
		}

		if _, err = flv.file.Seek(-(int64(size) + 4), 1); err != nil {
			return
		}

		_, err = io.ReadFull(flv.file, b1); if err != nil {
			return
		}
		ts :=
			(int(b1[7]) << 24) |
			(int(b1[4]) << 16) |
			(int(b1[5]) <<  8) |
			(int(b1[6])      )
		//fmt.Printf("ts: %d\n", ts)

		if b1[0] == 8 {
			flv.audioTimestamp = ts
			audioFound = true
		} else if b1[0] == 9 {
			flv.videoTimestamp = ts
			videoFound = true
		}

		if _, err = flv.file.Seek(-(11 + 4), 1); err != nil {
			return
		}

	}

	return
}

func (flv *Flv) writeHeader() (err error) {
	_, err = flv.file.Write([]byte{
		'F', 'L', 'V',
		1, // FLV version 1
		5, // Audio+Video tags are present
		0, 0, 0, 9, // DataOffset = 9
		0, 0, 0, 0, // PreviousTagSize0
	})
	return
}

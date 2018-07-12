package zip2mp4

import (
	"fmt"
	"archive/zip"
	"regexp"
	"strconv"
	"log"
	"sort"
	"io"
	"os"
	"os/exec"
	"io/ioutil"
	"path/filepath"
	"../files"
	"../log4gui"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"bytes"
)

type ZipMp4 struct {
	ZipName string
	Mp4NameOpened string

	FFMpeg *exec.Cmd
	FFStdin io.WriteCloser
}
var cmdListFF = []string{
	"./bin/ffmpeg/ffmpeg",
	"./bin/ffmpeg",
	"./ffmpeg/ffmpeg",
	"./ffmpeg",
	"ffmpeg",
}
var cmdListMP42TS = []string{
	"./bin/bento4/bin/mp42ts",
	"./bento4/bin/mp42ts",
	"./bento4/mp42ts",
	"./bin/bento4/mp42ts",
	"./bin/mp42ts",
	"./mp42ts",
	"mp42ts",
}
// return cmd = nil if cmd not exists
func openProg(cmdList *[]string, stdinEn, stdoutEn, stdErrEn, consoleEn bool, args []string) (cmd *exec.Cmd, stdin io.WriteCloser, stdout, stderr io.ReadCloser) {

	for i, cmdName := range *cmdList {
		cmd = exec.Command(cmdName, args...)

		var err error
		if stdinEn {
			stdin, err = cmd.StdinPipe()
			if err != nil {
				panic(err)
			}
		}

		if stdoutEn {
			stdout, err = cmd.StdoutPipe()
			if err != nil {
				panic(err)
			}
		} else {
			if consoleEn {
				cmd.Stdout = os.Stdout
			}
		}

		if stdErrEn {
			stderr, err = cmd.StderrPipe()
			if err != nil {
				panic(err)
			}
		} else {
			if consoleEn {
				cmd.Stderr = os.Stderr
			}
		}

		if err = cmd.Start(); err != nil {
			continue
		} else {
			if i != 0 {
				*cmdList = []string{cmdName}
			}
			//fmt.Printf("CMD: %#v\n", cmd.Args)
			return
		}
	}
	cmd = nil
	return
}
func MergeVA(vFileName, aFileName, oFileName string) bool {
	cmd, _, _, _ := openProg(&cmdListFF, false, false, false, true, []string{
		"-i", vFileName,
		"-i", aFileName,
		"-c", "copy",
		"-y",
		oFileName,
	})
	if cmd == nil {
		return false
	}
	if err := cmd.Wait(); err != nil {
		fmt.Println(err)
		return false
	}
	return true
}
func FFmpegExists() bool {
	cmd, _, _, _ := openProg(&cmdListFF, false, false, false, false, []string{"-version"})
	if cmd == nil {
		return false
	}
	cmd.Wait()
	return true
}
func GetFormat(fileName string) (vFormat, aFormat string) {
	cmd, _, stdout, stderr := openProg(&cmdListFF, false, true, true, false, []string{"-i", fileName})
	if cmd == nil {
		return
	}
	b1, _ := ioutil.ReadAll(stdout)
	b2, _ := ioutil.ReadAll(stderr)
	cmd.Wait()

	s := string(b1) + string(b2)
	if ma := regexp.MustCompile(`(?i)Stream\s+#.+?:\s+Video:\s+(.*?),`).FindStringSubmatch(s); len(ma) > 0 {
		vFormat = ma[1]
	}
	if ma := regexp.MustCompile(`(?i)Stream\s+#.+?:\s+Audio:\s+(.*?),`).FindStringSubmatch(s); len(ma) > 0 {
		aFormat = ma[1]
	}

	return
}
func openFFMpeg(stdinEn, stdoutEn, stdErrEn, consoleEn bool, args []string) (cmd *exec.Cmd, stdin io.WriteCloser, stdout, stderr io.ReadCloser) {
	return openProg(&cmdListFF, stdinEn, stdoutEn, stdErrEn, consoleEn, args)
}
func openMP42TS(consoleEn bool, args []string) (cmd *exec.Cmd) {
	cmd, _, _, _ = openProg(&cmdListMP42TS, false, false, false, consoleEn, args)
	return
}
func (z *ZipMp4) Wait() {
	if z.FFMpeg != nil {
		if err := z.FFMpeg.Wait(); err != nil {
			panic(err)
		}
		z.FFMpeg = nil
	}
}
func (z *ZipMp4) CloseFFInput() {
	z.FFStdin.Close()
}
func (z *ZipMp4) OpenFFMpeg() {
	name := regexp.MustCompile(`(?i)\.zip\z`).ReplaceAllString(z.ZipName, "")
	name = fmt.Sprintf("%s.mp4", name)
	dir := filepath.Dir(name)
	base := filepath.Base(name)
	base, err := files.GetFileNameNext(base)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	name = filepath.Join(dir, base)
	z.Mp4NameOpened = name

	cmd, stdin, _, _ := openFFMpeg(true, false, false, true, []string{
		"-i", "-",
		"-c", "copy",
		"-y",
		name,
	})
	if cmd == nil {
		panic("ffmpeg not installed")
	}

	z.FFMpeg = cmd
	z.FFStdin = stdin
}

func (z *ZipMp4) FFInputCombFromFile(videoFile, audioFile string) {

	vTs := fmt.Sprintf("%s.ts", videoFile)
	cmdV := openMP42TS(false, []string{
		videoFile,
		vTs,
	})
	if cmdV == nil {
		fmt.Println("mp42ts not found OR command failed")
		os.Exit(1)
	}
	defer os.Remove(vTs)

	aTs := fmt.Sprintf("%s.ts", audioFile)
	cmdA := openMP42TS(false, []string{
		audioFile,
		aTs,
	})
	if cmdA == nil {
		fmt.Println("mp42ts not found OR command failed")
		os.Exit(1)
	}
	defer os.Remove(aTs)

	if err := cmdV.Wait(); err != nil {
		panic(err)
	}
	if err := cmdA.Wait(); err != nil {
		panic(err)
	}

	cmd, _, stdout, _ := openFFMpeg(false, true, false, false, []string{
		"-i", vTs,
		"-i", aTs,
		"-c", "copy",
		"-f", "mpegts",
		"-",
	})
	if cmd == nil {
		panic("ffmpeg not installed")
	}

	z.FFInput(stdout)

	if err := cmd.Wait(); err != nil {
		panic(err)
	}
}
func (z *ZipMp4) FFInput(rdr io.Reader) {
	if _, err := io.Copy(z.FFStdin, rdr); err != nil {
		panic(err)
	}
}

type Index struct {
	int
}
type Chunk struct {
	VideoIndex *Index
	AudioIndex *Index
	VAIndex *Index
}

func Convert(fileName string) (err error) {
	zr, err := zip.OpenReader(fileName)
	if err != nil {
		return
	}

	chunks := make(map[int64]Chunk)

	for i, r := range zr.File {
		//fmt.Printf("X %v %v\n", i, r.Name)

		if ma := regexp.MustCompile(`\Avideo-(\d+)\.\w+\z`).FindStringSubmatch(r.Name); len(ma) > 0 {
			num, err := strconv.ParseInt(string(ma[1]), 10, 64)
			if err != nil {
				log.Fatal(err)
			}
			if v, ok := chunks[num]; ok {
				v.VideoIndex = &Index{i}
				chunks[num] = v
			} else {
				chunks[num] = Chunk{VideoIndex: &Index{i}}
			}

			//fmt.Printf("V %v %v\n", i, r.Name)
		} else if ma := regexp.MustCompile(`\Aaudio-(\d+)\.\w+\z`).FindStringSubmatch(r.Name); len(ma) > 0 {
			num, err := strconv.ParseInt(string(ma[1]), 10, 64)
			if err != nil {
				log.Fatal(err)
			}
			if v, ok := chunks[num]; ok {
				v.AudioIndex = &Index{i}
				chunks[num] = v
			} else {
				chunks[num] = Chunk{AudioIndex: &Index{i}}
			}
			//fmt.Printf("A %v %v\n", num, r.Name)
		} else if ma := regexp.MustCompile(`\A(\d+)\.\w+\z`).FindStringSubmatch(r.Name); len(ma) > 0 {
			num, err := strconv.ParseInt(string(ma[1]), 10, 64)
			if err != nil {
				log.Fatal(err)
			}
			if v, ok := chunks[num]; ok {
				v.VAIndex = &Index{i}
				chunks[num] = v
			} else {
				chunks[num] = Chunk{VAIndex: &Index{i}}
			}
			//fmt.Printf("V+A %v %v\n", num, r.Name)
		} else {
			fmt.Printf("%v %v\n", i, r.Name)
			log4gui.Info(fmt.Sprintf("Unsupported zip: %s", fileName))
			os.Exit(1)
		}
	}

	keys := make([]int64, 0, len(chunks))
	for k := range chunks {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var tmpVideoName string
	var tmpAudioName string

	var zm *ZipMp4
	defer func() {
		if zm != nil {
			zm.CloseFFInput()
			zm.Wait()
		}
	}()

	zm = &ZipMp4{ZipName: fileName}
	zm.OpenFFMpeg()

	prevIndex := int64(-1)
	for _, key := range keys {
		if prevIndex >= 0 {
			if key != prevIndex + 1 {
				// [FIXME] reopen new mp4file?
				//return fmt.Errorf("\n\nError: seq skipped: %d --> %d\n\n", prevIndex, key)

				fmt.Printf("\nSeqNo. skipped: %d --> %d\n", prevIndex, key)
				if zm != nil {
					zm.CloseFFInput()
					zm.Wait()
				}
				zm = &ZipMp4{ZipName: fileName}
				zm.OpenFFMpeg()
			}
		}
		prevIndex = key

		if chunks[key].VAIndex != nil {
			r, e := zr.File[chunks[key].VAIndex.int].Open()
			if e != nil {
				panic(e)
			}
			zm.FFInput(r)
			r.Close()

		} else if chunks[key].VideoIndex != nil && chunks[key].AudioIndex != nil {

			if tmpVideoName == "" {
				f, e := ioutil.TempFile(".", "__temp-")
				if e != nil {
					panic(e)
				}
				f.Close()
				tmpVideoName = f.Name()
			}
			if tmpAudioName == "" {
				f, e := ioutil.TempFile(".", "__temp-")
				if e != nil {
					panic(e)
				}
				f.Close()
				tmpAudioName = f.Name()
			}

			// open temporary file
			tmpVideo, err := os.Create(tmpVideoName)
			if err != nil {
				panic(err)
			}
			tmpAudio, err := os.Create(tmpAudioName)
			if err != nil {
				panic(err)
			}

			// copy Video to file
			rv, e := zr.File[chunks[key].VideoIndex.int].Open()
			if e != nil {
				panic(e)
			}
			if _, e := io.Copy(tmpVideo, rv); e != nil {
				panic(e)
			}
			rv.Close()
			tmpVideo.Close()

			// copy Audio to file
			ra, e := zr.File[chunks[key].AudioIndex.int].Open()
			if e != nil {
				panic(e)
			}
			if _, e := io.Copy(tmpAudio, ra); e != nil {
				panic(e)
			}
			ra.Close()
			tmpAudio.Close()

			// combine video + audio using ffmpeg(+mp42ts)
			zm.FFInputCombFromFile(tmpVideoName, tmpAudioName)
			os.Remove(tmpVideoName)
			os.Remove(tmpAudioName)
		} else {
			if (chunks[key].VideoIndex == nil && chunks[key].AudioIndex != nil) ||
				(chunks[key].VideoIndex != nil && chunks[key].AudioIndex == nil) {
					fmt.Printf("\nIncomplete sequence. skipped: %d\n", key)
					if zm != nil {
						zm.CloseFFInput()
						zm.Wait()
					}
					zm = &ZipMp4{ZipName: fileName}
					zm.OpenFFMpeg()
			}
		}
	}

	zm.CloseFFInput()
	zm.Wait()
	fmt.Printf("\nfinish: %s\n", zm.Mp4NameOpened)

	return
}


func ConvertDB(fileName string) (err error) {
	db, err := sql.Open("sqlite3", os.Args[1])
	if err != nil {
		return
	}
	defer db.Close()

	var zm *ZipMp4
	defer func() {
		if zm != nil {
			zm.CloseFFInput()
			zm.Wait()
		}
	}()

	zm = &ZipMp4{ZipName: fileName}
	zm.OpenFFMpeg()

	rows, err := db.Query("select seqno, size, data from media where data is not null order by seqno")
	if err != nil {
		return
	}
	defer rows.Close()

	prevIndex := int64(-1)
	for rows.Next() {

		var seqno int64
		var size int
		var data []byte
		err = rows.Scan(&seqno, &size, &data)
		if err != nil {
			return
		}

		if prevIndex >= 0 {
			if seqno != prevIndex + 1 {

				fmt.Printf("\nSeqNo. skipped: %d --> %d\n", prevIndex, seqno)
				if zm != nil {
					zm.CloseFFInput()
					zm.Wait()
				}
				zm = &ZipMp4{ZipName: fileName}
				zm.OpenFFMpeg()
			}
		}
		prevIndex = seqno

		zm.FFInput(bytes.NewBuffer(data))
	}

	zm.CloseFFInput()
	zm.Wait()
	fmt.Printf("\nfinish: %s\n", zm.Mp4NameOpened)

	return
}

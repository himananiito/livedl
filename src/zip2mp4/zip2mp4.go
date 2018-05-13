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
	"./bin/mp42ts",
	"./mp42ts",
	"mp42ts",
}
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

	z := &ZipMp4{ZipName: fileName}
	z.OpenFFMpeg()

	defer func() {
		z.CloseFFInput()
		z.Wait()
	}()

	chunks := make(map[int64]Chunk)

	for i, r := range zr.File {
		//fmt.Printf("X %v %v\n", i, r.Name)

			//return

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
			fmt.Printf("X %v %v\n", i, r.Name)
		}
	}

    keys := make([]int64, 0, len(chunks))
    for k := range chunks {
        keys = append(keys, k)
    }

	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var tmpVideoName string
	var tmpAudioName string

	prevIndex := int64(-1)
	for _, key := range keys {
		if prevIndex >= 0 {
			if key != prevIndex + 1 {
				// [FIXME] reopen new mp4file?
				return fmt.Errorf("\n\nError: seq skipped: %d --> %d\n\n", prevIndex, key)
			}
		}
		prevIndex = key

		if chunks[key].VAIndex != nil {
			r, e := zr.File[chunks[key].VAIndex.int].Open()
			if e != nil {
				panic(e)
			}
			z.FFInput(r)
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
			z.FFInputCombFromFile(tmpVideoName, tmpAudioName)
			os.Remove(tmpVideoName)
			os.Remove(tmpAudioName)
		}
	}

	z.CloseFFInput()
	z.Wait()
	fmt.Printf("\nfinish: %s\n", z.Mp4NameOpened)

	return
}
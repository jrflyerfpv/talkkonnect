/*
 * talkkonnect headless mumble client/gateway with lcd screen and channel control
 * Copyright (C) 2018-2019, Suvir Kumar <suvir@talkkonnect.com>
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/.
 *
 * Software distributed under the License is distributed on an "AS IS" basis,
 * WITHOUT WARRANTY OF ANY KIND, either express or implied. See the License
 * for the specific language governing rights and limitations under the
 * License.
 *
 * talkkonnect is the based on talkiepi and barnard by Daniel Chote and Tim Cooper
 *
 * The Initial Developer of the Original Code is
 * Suvir Kumar <suvir@talkkonnect.com>
 * Portions created by the Initial Developer are Copyright (C) Suvir Kumar. All Rights Reserved.
 *
 * Contributor(s):
 *
 * Suvir Kumar <suvir@talkkonnect.com>
 * Zoran Dimitrijevic
 * My Blog is at www.talkkonnect.com
 * The source code is hosted at github.com/talkkonnect
 *
 * avrecord.go -> talkkonnect function to record audio and video with low cost USB web cameras.
 * Record incoming Mumble traffic with sox package. Record video and images with external
 * packages fswebcam, motion, ffmpeg or other.
 *
 */

package talkkonnect

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"sync"
)

var (
	jobIsRunning   bool // used for mux for motion, fswebcam, ffmpeg, sox
	JobIsrunningMu sync.Mutex
)

// Record incoming Mumble traffic with sox

func AudioRecordTraffic() {

	// Need a way to prevent multiple sox instances running, or kill old one.
	_, err := exec.Command("sh", "-c", "killall -SIGINT sox").Output()
	if err != nil {
		log.Println("debug: No Old sox Instance is Running. It is OK to Start sox")
	} else {
		time.Sleep(1 * time.Second)
		log.Println("debug: Old sox instance was Killed Before Running New")
	}

	CreateDirIfNotExist(AudioRecordSavePath)
	CreateDirIfNotExist(AudioRecordArchivePath)
	emptydirchk, err := DirIsEmpty(AudioRecordSavePath)
	if err == nil && emptydirchk == false {

		filezip := time.Now().Format("20060102150405") + ".zip"
		go zipit(AudioRecordSavePath+"/", AudioRecordArchivePath+"/"+filezip)
		log.Println("debug: Archiving Old Audio Files to", AudioRecordArchivePath+"/"+filezip)
		time.Sleep(1 * time.Second)
		cleardir(AudioRecordSavePath)
	} else {
		log.Println("debug: Audio Recording Folder Is Empty. No Old Files to Archive")
	}
	time.Sleep(1 * time.Second)
	go audiorecordtraffic()
	log.Println("debug: sox is Recording Traffic to", AudioRecordSavePath)

	return
}

// Record ambient audio from microphone with sox

func AudioRecordAmbient() {

	CreateDirIfNotExist(AudioRecordSavePath)
	CreateDirIfNotExist(AudioRecordArchivePath)
	emptydirchk, err := DirIsEmpty(AudioRecordSavePath)
	if err == nil && emptydirchk == false {
		filezip := time.Now().Format("20060102150405") + ".zip"
		go zipit(AudioRecordSavePath+"/", AudioRecordArchivePath+"/"+filezip) // path to end with "/" or not?
		log.Println("info: Archiving Old Audio Files to", AudioRecordArchivePath+"/"+filezip)
		time.Sleep(1 * time.Second)
		cleardir(AudioRecordSavePath) // Remove old files
	} else {
		log.Println("debug: Audio Recording Folder Is Empty. No Old Files to Archive")
	}
	time.Sleep(1 * time.Second)
	go audiorecordambientmux()

	return
}

// Record both incoming Mumble traffic and ambient audio with sox

func AudioRecordCombo() {

	CreateDirIfNotExist(AudioRecordSavePath)
	CreateDirIfNotExist(AudioRecordArchivePath)
	emptydirchk, err := DirIsEmpty(AudioRecordSavePath)
	if err == nil && emptydirchk == false {
		filezip := time.Now().Format("20060102150405") + ".zip"
		go zipit(AudioRecordSavePath+"/", AudioRecordArchivePath+"/"+filezip)
		log.Println("info: Archiving Old Audio Files to", AudioRecordArchivePath+"/"+filezip)
		time.Sleep(1 * time.Second)
		cleardir(AudioRecordSavePath)
	} else {
		log.Println("debug: Audio Recording Folder Is Empty. No Old Files to Archive")
	}
	time.Sleep(1 * time.Second)
	go audiorecordcombomux()

	return
}

//Record traffic with mux exclusion. Allow new start only if currently not running.

func audiorecordtrafficmux() { // check if mux for this is working?

	JobIsrunningMu.Lock()
	start := !jobIsRunning
	jobIsRunning = true
	JobIsrunningMu.Unlock()

	if start {
		go func() {
			audiorecordtraffic()
			JobIsrunningMu.Lock()
			jobIsRunning = false
			JobIsrunningMu.Unlock()
		}()
	} else {
		log.Println("info: Traffic Audio Recording is Already Running. Please Wait.")
	}
}

//  sox function for traffic recording

func audiorecordtraffic() {

	// check if external program is installed?
	checkfile := isCommandAvailable("/usr/bin/sox")
	if checkfile == false {
		log.Println("error: sox is Missing. Is the Package Installed?")
	}

	audrecfile := time.Now().Format("20060102150405") + "." + AudioRecordFileFormat
	log.Println("info: sox is Recording Traffic to", AudioRecordSavePath+"/"+audrecfile)
	log.Println("info: Audio Recording Mode:", AudioRecordMode)

	if AudioRecordTimeout != 0 { // Record traffic, but stop it after timeout, if specified. "0" for no timeout.

		args := []string{"-t", AudioRecordSystem, AudioRecordFromOutput, "-t", AudioRecordFileFormat, audrecfile, "trim", "0", AudioRecordChunkSize, ":", "newfile", ":", "restart"}

		log.Println("debug: sox Arguments: " + fmt.Sprint(strings.Trim(fmt.Sprint(args), "[]")))
		log.Println("debug: Traffic Recording will Timeout After:", AudioRecordTimeout, "seconds")

		cmd := exec.Command("/usr/bin/sox", args...)
		cmd.Dir = AudioRecordSavePath
		err := cmd.Start()
		check(err)
		done := make(chan struct{})

		time.Sleep(time.Duration(AudioRecordTimeout) * time.Second) // let sox record for a time, then send kill signal
		go func() {
			err := cmd.Wait()
			status := cmd.ProcessState.Sys().(syscall.WaitStatus)
			exitStatus := status.ExitStatus()
			signaled := status.Signaled()
			signal := status.Signal()
			log.Println("error: sox Error:", err)
			if signaled {
				log.Println("debug: sox Signal:", signal)
			} else {
				log.Println("debug: sox Status:", exitStatus)
			}
			close(done)
			// Did sox close ?
			log.Println("info: sox Stopped Recording Traffic to", AudioRecordSavePath)
		}()
		cmd.Process.Kill()
		<-done

	} else { // if AudioRecordTimeout is zero? Just keep recording until there is disk space on media.

		audrecfile := time.Now().Format("20060102150405") + "." + AudioRecordFileFormat // mp3, wav

		args := []string{"-t", AudioRecordSystem, AudioRecordFromOutput, "-t", "mp3", audrecfile, "silence", "-l", "1", "1", "2%", "-1", "0.5", "2%", "trim", "0", AudioRecordChunkSize, ":", "newfile", ":", "restart"}

		cmd := exec.Command("/usr/bin/sox", args...)
		cmd.Dir = AudioRecordSavePath
		err := cmd.Start()
		check(err)
		time.Sleep(2 * time.Second)

		emptydirchk, err := DirIsEmpty(AudioRecordSavePath) // If sox didn't start recording for wrong parameters or any reason...  No  file.

		if err == nil && emptydirchk == false {
			log.Println("info: sox is Recording Traffic to", AudioRecordSavePath)
			log.Println("info: sox will Go On Recording, Until it Runs out of Space or is Interrupted")

			starttime := time.Now()
			ticker := time.NewTicker(300 * time.Second) // Reminder if sox recording program is still recording after ... 5 minutes (no timeout)

			go func() {
				for range ticker.C {
					checked := time.Since(starttime)
					checkedshort := fmt.Sprintf(before(fmt.Sprint(checked), ".")) // trim  milliseconds after.  Format 00h00m00s.
					elapsed := fmtDuration(checked) // hh:mm format
					log.Println("debug: sox is Still Running After", checkedshort+"s", "|", elapsed)
				}
			}()

		} else {
			log.Println("error: Something Went Wrong... sox Traffic Recording was Launched but Encountered Some Problems")
			log.Println("warn: Check ALSA Sound Settings and sox Arguments")
		}
	}
}

// If talkkonnect stops or hangs. Must close sox manually. No signaling to sox for closing in this case.
//Record traffic and Mic mux exclusion.  Allow new start only if currently not running.

func audiorecordambientmux() {

	JobIsrunningMu.Lock()
	start := !jobIsRunning
	jobIsRunning = true
	JobIsrunningMu.Unlock()

	if start {
		go func() {
			audiorecordambient()
			JobIsrunningMu.Lock()
			jobIsRunning = false
			JobIsrunningMu.Unlock()
		}()
	} else {
		log.Println("info: Ambient Audio Recording is Already Running. Please Wait.")
	}
}

// sox function for ambient recording

func audiorecordambient() {

	checkfile := isCommandAvailable("/usr/bin/sox")
	if checkfile == false {
		log.Println("error: sox is Missing. Is the Package Installed?")
	}

	//Need apt-get install sox libsox-fmt-mp3 (lame)

	audrecfile := time.Now().Format("20060102150405") + "." + AudioRecordFileFormat // mp3, wav

	log.Println("info: sox is Recording Ambient Audio to", AudioRecordSavePath+"/"+audrecfile)
	log.Println("info: sox Audio Recording will Stop After", fmt.Sprint(AudioRecordMicTimeout), "seconds")

	if AudioRecordMicTimeout != 0 { // Record ambient audio, but stop it after timeout, if specified. "0" no timeout.

		args := []string{"-t", AudioRecordSystem, AudioRecordFromInput, "-t", "mp3", audrecfile, "trim", "0", AudioRecordChunkSize, ":", "newfile", ":", "restart"}

		cmd := exec.Command("/usr/bin/sox", args...)

		cmd.Dir = AudioRecordSavePath // save audio recording
		err := cmd.Start()
		check(err)
		done := make(chan struct{})
		time.Sleep(time.Duration(AudioRecordMicTimeout) * time.Second) // let sox record for a time, then signal kill

		go func() {
			err := cmd.Wait()
			status := cmd.ProcessState.Sys().(syscall.WaitStatus)
			exitStatus := status.ExitStatus()
			signaled := status.Signaled()
			signal := status.Signal()
			log.Println("error: sox Error:", err)
			if signaled {
				log.Println("debug: sox Signal:", signal)
			} else {
				log.Println("debug: sox Status:", exitStatus)
			}
			close(done)
			// Did sox close ?
			log.Println("info: sox Stopped Recording Traffic to", AudioRecordSavePath)
		}()
		cmd.Process.Kill()
	} else {
		audrecfile := time.Now().Format("20060102150405") + "." + AudioRecordFileFormat // mp3, wav

		args := []string{"-t", AudioRecordSystem, AudioRecordFromInput, "-t", "mp3", audrecfile, "silence", "-l", "1", "1", `2%`, "-1", "0.5", `2%`, "trim", "0", AudioRecordChunkSize, ":", "newfile", ":", "restart"} // voice detect, trim silence with 5 min audio chunks

		cmd := exec.Command("/usr/bin/sox", args...)
		cmd.Dir = AudioRecordSavePath // save audio recording to dir
		err := cmd.Start()
		check(err)

		emptydirchk, err := DirIsEmpty(AudioRecordSavePath) // If sox didn't start recording for wrong parameters or any reason...  No file.

		if err == nil && emptydirchk == false {
			log.Println("info: sox is Recording Ambient Audio to", AudioRecordSavePath)
			log.Println("warn: sox will Go On Recording, Until it Runs out of Space or is Interrupted")

			starttime := time.Now()

			ticker := time.NewTicker(300 * time.Second) // reminder if program is still recording after ... 5 minutes

			go func() {
				for range ticker.C {
					checked := time.Since(starttime)
					checkedshort := fmt.Sprintf(before(fmt.Sprint(checked), ".")) // trim  milliseconds after .  Format 00h00m00s.
					elapsed := fmtDuration(checked) // hh:mm format
					log.Println("info: sox is Still Running After", checkedshort+"s", "|", elapsed)
				}
			}()

		} else {
			log.Println("error: Something Went Wrong... sox Traffic Recording was Launched but Encountered Some Problems")
			log.Println("warn: Check ALSA Sound Settings and sox Arguments")
		}
	}
}

//Record traffic and Mic mux exclusion.  Allow new start only if currently not running.

func audiorecordcombomux() {

	JobIsrunningMu.Lock()
	start := !jobIsRunning
	jobIsRunning = true
	JobIsrunningMu.Unlock()

	if start {
		go func() {
			audiorecordcombo()
			JobIsrunningMu.Lock()
			jobIsRunning = false
			JobIsrunningMu.Unlock()
		}()
	} else {
		log.Println("info: Combo Audio Recording is Already Running. Please Wait.")
	}
}

// Record traffic and Mic.

func audiorecordcombo() {

	checkfile := isCommandAvailable("/usr/bin/sox")
	if checkfile == false {
		log.Println("error: sox is Missing. Is the Package Installed?")
	}

	//Need apt-get install sox libsox-fmt-mp3 (lame)

	audrecfile := time.Now().Format("20060102150405") + "." + AudioRecordFileFormat
	log.Println("info: sox is Recording Traffic to", AudioRecordSavePath+"/"+audrecfile)
	log.Println("info: Audio Recording Mode:", AudioRecordMode)

	if AudioRecordTimeout != 0 { // Record traffic, but stop it after timeout, if specified. "0" no timeout.

		args := []string{"-m", "-t", AudioRecordSystem, AudioRecordFromOutput, "-t", AudioRecordSystem, AudioRecordFromInput, "-t", AudioRecordFileFormat, audrecfile, "silence", "-l", "1", "1", `2%`, "-1", "0.5", `2%`, "trim", "0", AudioRecordChunkSize, ":", "newfile", ":", "restart"}

		log.Println("debug: sox Arguments: " + fmt.Sprint(strings.Trim(fmt.Sprint(args), "[]")))
		log.Println("info: Audio Combo Recording will Timeout After:", AudioRecordTimeout, "seconds")

		cmd := exec.Command("/usr/bin/sox", args...)
		cmd.Dir = AudioRecordSavePath
		err := cmd.Start()
		check(err)
		done := make(chan struct{})

		time.Sleep(time.Duration(AudioRecordTimeout) * time.Second) // let sox record for a time, then send kill signal

		go func() {
			err := cmd.Wait()
			status := cmd.ProcessState.Sys().(syscall.WaitStatus)
			exitStatus := status.ExitStatus()
			signaled := status.Signaled()
			signal := status.Signal()
			log.Println("erroe: sox Error:", err)
			if signaled {
				log.Println("debug: sox Signal:", signal)
			} else {
				log.Println("debug: sox Status:", exitStatus)
			}
			close(done)
			// Did sox close ?
			log.Println("info: sox Stopped Recording Traffic to", AudioRecordSavePath)
		}()
		cmd.Process.Kill()
		<-done

	} else { // if AudioRecordTimeout is zero? Just keep recording until there is disk space on media.

		audrecfile := time.Now().Format("20060102150405") + "." + AudioRecordFileFormat // mp3, wav

		args := []string{"-m", "-t", AudioRecordSystem, AudioRecordFromOutput, "-t", AudioRecordSystem, AudioRecordFromInput, "-t", "mp3", audrecfile, "silence", "-l", "1", "1", `2%`, "-1", "0.5", `2%`, "trim", "0", AudioRecordChunkSize, ":", "newfile", ":", "restart"}

		cmd := exec.Command("/usr/bin/sox", args...)
		cmd.Dir = AudioRecordSavePath
		err := cmd.Start()
		check(err)
		time.Sleep(2 * time.Second)

		emptydirchk, err := DirIsEmpty(AudioRecordSavePath) // If sox didn't start recording for wrong parameters or any reason...  No files.

		if err == nil && emptydirchk == false {
			log.Println("info: sox is Recording Mixed Audio to", AudioRecordSavePath)
			log.Println("warn: sox will Go On Recording, Until it Runs out of Space or is Interrupted")

			starttime := time.Now()

			ticker := time.NewTicker(300 * time.Second) // Reminder if sox recordin program is still recording after ... 5 minutes (no timeout)

			go func() {
				for range ticker.C {
					checked := time.Since(starttime)
					checkedshort := fmt.Sprintf(before(fmt.Sprint(checked), ".")) // trim  milliseconds after .  Format 00h00m00s.
					elapsed := fmtDuration(checked) // hh:mm format
					log.Println("info: sox is Still Running After", checkedshort+"s", "|", elapsed)
				}
			}()

		} else {
			log.Println("error: Something Went Wrong... sox Traffic Recording was Launched but Encountered Some Problems")
			log.Println("warn: Check ALSA Sound Settings and sox Arguments")
		}
	}
}

//

func clearfiles() { // Testing os.Remove to delete files
	err := os.RemoveAll(`/avrec`)
	if err != nil {
		fmt.Println(err)
		return
	}
}

// mux for server

func fileserve3mux() {

	JobIsrunningMu.Lock()
	start := !jobIsRunning
	jobIsRunning = true
	JobIsrunningMu.Unlock()

	if start {
		go func() {
			fileserve3()
			JobIsrunningMu.Lock()
			jobIsRunning = false
			JobIsrunningMu.Unlock()
		}()
	} else {
		log.Println("info: Ambient Audio Recording is Already Running. Please Wait.")
	}
}

// Serve audio recordings over 8085

func fileserve3() {
	port := flag.String("psox", "8085", "port to serve on")
	directory := flag.String("dsox", AudioRecordSavePath, "the directory of static file to host")
	//. "dot" or / or ./img or AudioRecordSavePath, AudioRecordArchivePath
	flag.Parse()
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(*directory)))
	//http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(http.Dir("./img/"))))
	// in case of problem with img dir
	time.Sleep(5 * time.Second)
	log.Println("debug: Serving Audio Files", *directory, "over HTTP port:", *port)
	log.Println("info: HTTP Server Waiting")
	// log.Fatal(http.ListenAndServe(":" + *port, nil))
	log.Fatal(http.ListenAndServe(":"+*port, mux))
}

func fileserve4() {
	port := flag.String("pavrec", "8086", "port to serve on")
	directory := flag.String("davrec", "/avrec", "the directory of static file to host")
	flag.Parse()
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(*directory)))
	//http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(http.Dir("./img/"))))
	// in case of problem with img dir
	time.Sleep(5 * time.Second)
	log.Println("debug: Serving Directory", *directory, "over HTTP port:", *port)
	log.Println("info: HTTP Server Waiting")
	// log.Fatal(http.ListenAndServe(":" + *port, nil))
	log.Fatal(http.ListenAndServe(":"+*port, mux))
}

func zipit(source, target string) error {
	zipfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipfile.Close()

	archive := zip.NewWriter(zipfile)
	defer archive.Close()

	info, err := os.Stat(source)
	if err != nil {
		return nil
	}

	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base(source)
	}

	filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		if baseDir != "" {
			header.Name = filepath.Join(baseDir, strings.TrimPrefix(path, source))
		}

		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})

	return err
}

// Unzip. For future use.

func unzip(archive, target string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}

	for _, file := range reader.File {
		path := filepath.Join(target, file.Name)
		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.Mode())
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return err
		}
		defer fileReader.Close()

		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		if _, err := io.Copy(targetFile, fileReader); err != nil {
			return err
		}
	}

	return nil
}

// Helper to check if dirs for working with images/video exist. If not create.

func CreateDirIfNotExist(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0777)
		if err != nil {
			panic(err)
		}
	}
}

// Helper to Clear files from work dir

func ClearDir(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}
	for _, file := range files {
		err = os.RemoveAll(file)
		if err != nil {
			//os.RemoveAll(dir) //  can do dir's
			return err
		}
	}
	return nil
}

// Another function to os.Remove, delete all files in dir.

func cleardir(dir string) {
	// The target directory.
	//directory := CamImageSavePath	// path must end on "/"... fix for no "/"?
	directory := dir + "/" // path with "/"
	// Open the directory and read all its files.
	dirRead, _ := os.Open(directory)
	dirFiles, _ := dirRead.Readdir(0)
	// Loop over the directory's files.
	for index := range dirFiles {
		fileHere := dirFiles[index]
		// Get name of file and its full path.
		nameHere := fileHere.Name()
		fullPath := directory + nameHere
		// Remove the files.
		os.Remove(fullPath)
		log.Println("info: Removed file", fullPath)

	}
}

// Helper to check is directory empty?

func DirIsEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err // Not Empty
		log.Println("debug: Dir is Not Empty", "%t")
	}
	defer f.Close()

	_, err = f.Readdirnames(1) // Or f.Readdir(1)  // empty
	if err == io.EOF {
		return true, nil
		log.Println("debug: Dir is Empty", "%t")
	}
	return false, err // Either not empty or error, suits both cases
}

// Check if some file exists or not. Maybe use later.

func FileExist(path string) bool {
	if _, err := os.Stat(path); err == nil {
		// exist
		return true
	}
	// not exist
	return false
}

func FileNotExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// not exist
		return true
	}
	// exist
	return false
}

// Check if fswebcam, motion or other bin is available in system?
// Dont start function if they ar not installed.

func isCommandAvailable(name string) bool {
	cmd := exec.Command("/bin/sh", "-c", "command -v "+name)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// Simple err check help for cmd
func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

//  Helper to round up duration time to 1h1m45s / 01:02 format
//  use when fmt printing sox recording times
func fmtDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	//d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	//s := m / time.Second
	return fmt.Sprintf("%02d:%02d", h, m) // show sec’s also?
}

// try to use time.Duration() and time.ParseDuration().time.String()
// instead to round up time format?

// Return before, between or after some strings.
// Trim for extracting desired values.

func before(value string, a string) string { // used for sox time
	// Get substring before a string.
	pos := strings.Index(value, a)
	if pos == -1 {
		return ""
	}
	return value[0:pos]
}

func between(value string, a string, b string) string {
	// Get substring between two strings.
	posFirst := strings.Index(value, a)
	if posFirst == -1 {
		return ""
	}
	posLast := strings.Index(value, b)
	if posLast == -1 {
		return ""
	}
	posFirstAdjusted := posFirst + len(a)
	if posFirstAdjusted >= posLast {
		return ""
	}
	return value[posFirstAdjusted:posLast]
}

func after(value string, a string) string {
	// Get substring after a string.
	pos := strings.LastIndex(value, a)
	if pos == -1 {
		return ""
	}
	adjustedPos := pos + len(a)
	if adjustedPos >= len(value) {
		return ""
	}
	return value[adjustedPos:len(value)]
}

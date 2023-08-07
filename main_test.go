// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const tmpDir = "tmp"
const goCompileError = `#! /usr/bin/env gorun

package main

import "fmt"

func main() {
}
`

const goCmdlineArgs = `#! /usr/bin/env gorun

package main

import "fmt"
import "os"

func main() {
   if len(os.Args) > 1 {
       fmt.Printf("a1=%s\n", os.Args[1])
   }
   if len(os.Args) > 2 {
       fmt.Printf("a2=%s\n", os.Args[2])
   }
   fmt.Printf("env A=%s\n", os.Getenv("A"))
}
`

func ensureDir(dir string) {
	// assume many will race here
	// => only care that result is a dir
	_, err := os.Stat(dir)
	if err != nil {
		os.Mkdir(dir, 0755)
	}
}
func runArgs(t *testing.T, exefile, code string, args []string) (string, error) {
	s, err := gorun(t, exefile, code, args, "")
	return s, err
}

/*
	func run2(exefile, code string) (string, error) {
		s, err := run(exefile, code, []string{}, "")
		return s, err
	}
*/
func gorun(t *testing.T, gofilename string, code string, args []string, extraEnv string) (string, error) {
	// exefile is actually .go file with #! /usr/bin/env gorun
	dx := filepath.Dir(gofilename)
	if dx != "" && dx != "." {
		panic(fmt.Sprintf("bad gofilename: %s - dir = %s", gofilename, dx))
	}
	exefile := filepath.Join(tmpdir(t), gofilename)

	err := os.WriteFile(exefile, []byte(code), 0755)
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exefile, args...)
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("PATH=%s:%s", cwd, os.Getenv("PATH")))
	if extraEnv != "" {
		cmd.Env = append(cmd.Env, extraEnv)
	}

	var out bytes.Buffer
	//var outerr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out //err

	err = cmd.Run()
	s := out.String()

	if err != nil {
		return s, err
	}
	return s, nil
}

func TestMain(m *testing.M) {

	// https://pkg.go.dev/testing#hdr-Main
	// = if present, only this function will run and m.Run() will run the tests
	// call flag.Parse() here if TestMain uses flags
	_, err := exec.Command("go", "build").CombinedOutput()
	if err != nil {
		fmt.Println("go build failed")
		os.Exit(7)
	}
	ensureDir(tmpDir)
	os.Exit(m.Run())
}

func tmpdir(t *testing.T) string {

	d := os.Getenv("GORUN_TESTDIR")
	if d != "" {
		ensureDir(d)
		return d
	}
	return t.TempDir()
}

func TestCompileError(t *testing.T) {
	t.Parallel()
	s, err := gorun(t, "compile-error", goCompileError, []string{}, "")

	if err == nil {
		t.Error("expected compile error")
		return
	}

	if strings.Contains(s, `"fmt" imported and not used`) {
		return // pass
	}
	t.Error("expected error message")
}

func TestCmdlineArgs(t *testing.T) {
	t.Parallel()
	s, err := gorun(t, "cmdline-args", goCmdlineArgs, []string{"900"}, "A=700")
	if err != nil {
		t.Errorf("exe failed - %s", err)
		return
	}
	if !strings.Contains(s, `a1=900`) {
		fmt.Printf("s=%s", s)
		t.Errorf("arg1 not found")
		return
	}
	if !strings.Contains(s, `A=700`) {
		t.Errorf("env not found")
		return
	}
}

func goUpdateString() string {
	dat, err := os.ReadFile("main_test.go")
	if err != nil {
		panic(err)
	}
	s := string(dat)

	const key = "-shared-code-fm--"
	k := strings.Index(s, "// --begin"+key)
	k2 := strings.Index(s, "// --end"+key)
	if k < 0 {
		panic("k < 0")
	}
	if k2 < 0 {
		panic("k2 < 0")
	}
	goString := `#! /usr/bin/env gorun
package main
import "fmt"

import "os"
import "strings"
import "time"
import "errors"

` + s[k:k2] + `
func main() {
	uid := os.Args[1]
	fm := create("client", os.Args[2])
	

	fm.sendMsg("colorX ready")
	fm.sendMsg("colorX m2")
	fm.wait("colorX exit")
	fmt.Printf("colorX uid %s\n", uid)
}
`

	return goString
}

// --begin-shared-code-fm--
type fileMsgs struct {
	role     string
	filename string
	begin    int
	out      string
}

func deleteFileIfExists(filename string) {
	// postcondition: file does not exist
	_, err := os.Stat(filename)
	if err == nil {
		err = os.Remove(filename)
		if err != nil {
			panic(err)
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			panic(err)
		}
	}
}
func create(role string, filename string) *fileMsgs {
	fm := fileMsgs{role, filename, 0, ""}
	// e.g. we want ensure clean start
	if role == "server" {
		deleteFileIfExists(fm.rxFile())
		deleteFileIfExists(fm.txFile())
	} else if role == "client" {

	} else {
		panic("unknown role " + role)
	}
	return &fm
}
func (f *fileMsgs) rxFile() string {
	if f.role == "server" {
		return f.filename + "-rx"
	}
	return f.filename + "-tx"
}
func (f *fileMsgs) txFile() string {
	if f.role == "server" {
		return f.filename + "-tx"
	}
	return f.filename + "-rx"
}

func (f *fileMsgs) readMsg() string {
	dat, err := os.ReadFile(f.rxFile())
	if err == nil {
		s := string(dat)
		if len(s) > f.begin {
			i := strings.Index(s[f.begin:], "\n")
			if i > 0 {
				msg := s[f.begin : f.begin+i]
				f.begin = f.begin + i + 1
				return msg
			}
		}
	}
	return ""
}

func (f *fileMsgs) wait(expect string) {
	start := time.Now()
	for {
		m := f.readMsg()
		if len(m) > 0 {
			//fmt.Printf("%d : rx msg %s\n", f.begin, m)
			if m == expect {
				break
			} else {
				panic("received " + m + " but expected " + expect)
			}
		}
		if time.Since(start) > 15*time.Second {
			panic("timeout waiting for msg")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fileMsgs) sendMsg(msg string) {
	f.out = f.out + fmt.Sprintf("%s\n", msg)
	err := os.WriteFile(f.txFile(), []byte(f.out), 0644)
	if err != nil {
		panic(err)
	}
}

// --end-shared-code-fm--

type updateMsg struct {
	msg string
	err error
}

func runUpdate(t *testing.T, ch chan updateMsg, color string, args []string) error {
	s, err := runArgs(t, "update", strings.ReplaceAll(goUpdateString(), "colorX", color), args)
	if err != nil {
		fmt.Printf("runUpdate: ERROR; %v - %s\n", err, s)
		ch <- updateMsg{s, err}
		return err
	}
	ch <- updateMsg{s, nil} // send msg
	return nil
}

func TestUpdateWhileRunning(t *testing.T) {
	workdir := tmpdir(t)
	t.Parallel()
	rand.Seed(time.Now().UnixNano())
	uid := fmt.Sprintf("%d", rand.Intn(100_000_000))
	//fmt.Printf("uid %s\n", uid)
	ch := make(chan updateMsg, 10)

	// use simple file-based messaging
	blueFM := create("server", filepath.Join(workdir, "blue"))
	redFM := create("server", filepath.Join(workdir, "red"))

	go runUpdate(t, ch, "blue", []string{uid, blueFM.filename})
	blueFM.wait("blue ready")
	blueFM.wait("blue m2")

	go runUpdate(t, ch, "red", []string{uid, redFM.filename})
	redFM.wait("red ready")
	redFM.wait("red m2")
	blueFM.sendMsg("blue exit")

	msg1 := <-ch

	redFM.sendMsg("red exit")
	msg2 := <-ch
	if msg1.err != nil {
		t.Errorf("ERROR: msg1: %s %s\n", msg1.msg, msg1.err)
		return
	}
	if msg2.err != nil {
		t.Errorf("ERROR: msg2: %s %s\n", msg2.msg, msg2.err)
		return
	}
	if msg1.msg != "blue uid "+uid+"\n" {
		t.Errorf("rx msg1: %s\n", msg1.msg)
		return
	}
	if msg2.msg != "red uid "+uid+"\n" {
		t.Errorf("rx msg2: %s\n", msg2.msg)
		return
	}
	//fmt.Printf("rx msg1: %s\n", msg1.msg)
	//fmt.Printf("rx msg2: %s\n", msg2.msg)
}

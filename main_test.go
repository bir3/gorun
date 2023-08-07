// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	wd, err := os.Getwd()
	if err != nil {
		os.Exit(8)
	}
	// https://pkg.go.dev/testing#hdr-Main
	// = if present, only this function will run and m.Run() will run the tests
	// call flag.Parse() here if TestMain uses flags
	s, err := exec.Command("go", "build").CombinedOutput()
	if err != nil {
		fmt.Printf("### go build failed: %s\ncwd=%s\n", s, wd)
		os.Exit(9)
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
	//t.Parallel()

	s, err := gorun(t, "compile-error", goCompileError, []string{}, "")

	if err == nil {
		t.Error("expected compile error")
		return
	}

	if strings.Contains(s, `"fmt" imported and not used`) {
		return // pass
	}
	t.Errorf("expected error message, got=%s", s)
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

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

	"github.com/bir3/gocompiler"
	"github.com/bir3/gorun"
	"github.com/bir3/gorun/cache"
)

func tmpdir(t *testing.T) string {

	d := os.Getenv("GORUN_TESTDIR")
	if d != "" {

		os.Mkdir(d, 0777) // assume many will race, so ignore error
		return d
	}
	return t.TempDir()
}

func gorunTest(t *testing.T, gofilename string, code string, args []string, extraEnv string) (string, error) {
	// gofilename is actually a .go file with #! /usr/bin/env gorun
	dx := filepath.Dir(gofilename)
	if dx != "" && dx != "." {
		panic(fmt.Sprintf("bad gofilename: %s - dir = %s", gofilename, dx))
	}
	exefile := filepath.Join(tmpdir(t), gofilename)

	err := os.WriteFile(exefile, []byte(code), 0777)
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

	cmd.Stdout = &out
	cmd.Stderr = &out

	err = cmd.Run()
	s := out.String()

	if err != nil {
		return s, err
	}
	return s, nil
}

func TestMain(m *testing.M) {

	// the go toolchain is built into the executable and must be given a chance to run
	// => avoid side effects in init() as they will occur multiple times during compilation
	if gocompiler.IsRunToolchainRequest() {
		gocompiler.RunToolchain()
		return
	}

	if len(os.Args) == 2 && os.Args[1] == "-test-execstring" {
		testExecString() // normally does not return
		os.Exit(0)
	}

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

	os.Exit(m.Run())
}

func TestCompileStdin(t *testing.T) {
	t.Parallel()
	goSimple := `#! /usr/bin/env gorun

	package main
	
	import "fmt"
	
	func main() {
		fmt.Printf("stdin\n")
	}`

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// setting PATH Env in cmd is not used for executable lookup
	// => must provide absolute path
	gorun := filepath.Join(cwd, "gorun")

	cmd := exec.Command(gorun, "-")

	var out bytes.Buffer
	cmd.Stdin = strings.NewReader(goSimple)
	cmd.Stdout = &out
	cmd.Stderr = &out

	err = cmd.Run()
	if err != nil {
		fmt.Println(out.String())
		t.Fatal(err)
	}
	s := out.String()
	expect := "stdin\n"
	if s != expect {
		t.Fatalf("got %s but expected %s", s, expect)
	}
}

func TestCompileError(t *testing.T) {
	t.Parallel()
	goCompileError := `#! /usr/bin/env gorun

	package main
	
	import "fmt"
	
	func main() {
	}
	`

	s, err := gorunTest(t, "compile-error", goCompileError, []string{}, "")

	if err == nil {
		t.Error("expected compile error")
		return
	}

	if !strings.Contains(s, `"fmt" imported and not used`) {
		t.Errorf("expected error message, got=%s", s)
	}

}

func TestCmdlineArgs(t *testing.T) {
	t.Parallel()
	goCmdlineArgs := `#! /usr/bin/env gorun

	package main
	
	import "fmt"
	import "os"
	
	func main() {
	   if len(os.Args) > 1 {
		   fmt.Printf("arg1=%s\n", os.Args[1])
	   }
	   if len(os.Args) > 2 {
		   fmt.Printf("arg2=%s\n", os.Args[2])
	   }
	   fmt.Printf("env A=%s\n", os.Getenv("A"))
	}
	`

	s, err := gorunTest(t, "cmdline-args", goCmdlineArgs, []string{"900"}, "A=700")
	if err != nil {
		t.Errorf("exe failed - %s", err)
		return
	}
	if !strings.Contains(s, `arg1=900`) {
		fmt.Printf("s=%s", s)
		t.Errorf("arg1 not found")
		return
	}
	if !strings.Contains(s, `env A=700`) {
		t.Errorf("env not found")
		return
	}
}

func TestExecString(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Errorf("Executable() failed : %s", err)
	}
	// ExecString does exec => must test in a subprocess
	cmd := exec.Command(exe, "-test-execstring")
	buf, err := cmd.Output()
	if err != nil {
		t.Errorf("%s", err)
	}
	if !strings.Contains(string(buf), `RunString OK`) {
		fmt.Printf("output buf=%s", string(buf))
		t.Errorf("missing magic string")
		return
	}

}

func testExecString() {
	goCode := `package main
import "fmt"
func main() {
	fmt.Println("RunString OK")
}
`
	config, err := cache.DefaultConfig()
	if err != nil {
		fmt.Printf("cache init failed: %s\n", err)
		os.Exit(7)
	}
	args := []string{}
	outdir, err := gorun.CompileString(config, goCode, args, "")
	if err != nil {
		fmt.Printf("RunString failed - %s\n", err)
		os.Exit(8)
	}
	exefile := filepath.Join(outdir, "main")
	// no lock => only thing protecting the executable is a recent timestamp
	err = gorun.Exec(exefile, args)
	fmt.Printf("RunString should never return, error = %s\n", err)
	os.Exit(9)
}

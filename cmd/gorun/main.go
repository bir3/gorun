// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bir3/gocompiler"
	"github.com/bir3/gorun"
	"github.com/bir3/gorun/cache"
)

func readFileAndStrip(filename string) string {
	var s string
	if filename == "-" {
		var out bytes.Buffer
		_, err := io.Copy(&out, os.Stdin)
		if err != nil {
			errExit(fmt.Sprintf("%s", err))
		}
		s = out.String()
	} else {
		b, err := os.ReadFile(filename)
		if err != nil {
			errExit(fmt.Sprintf("failed to read file %s", filename))
		}
		s = string(b)
	}

	if strings.HasPrefix(s, "#!") {
		i := strings.Index(s, "\n")
		if i < 0 {
			log.Fatal(errors.New("empty file"))
		}
		s = s[i+1:]
	}
	return s
}

func errExit(msg string) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", msg)
	os.Exit(3)
}

func showUsage() {
	helpStr := `
usage:
  gorun [gorun options] <filename> [program options]

  -h     show this help
  -v     show version
  -c     show cache size
  -show  show code cache location
  -shell enter shell at cache location
  -trim  clean cache now

  filename or "-" for stdin; first line can be #! /usr/bin/env gorun
`
	fmt.Printf("%s\n", strings.TrimSpace(helpStr))

}

func showCacheUsage() {
	c, err := cache.DefaultConfig()

	if err != nil {
		errExit(fmt.Sprintf("cache init failed: %s", err))
	}
	info, err := c.GetInfo()
	if err != nil {
		errExit(fmt.Sprintf("cache stat error : %s", err))
	}
	fmt.Printf("cache size is %d MB for %d items in %s\n", info.SizeBytes/1e6, info.Count, info.Dir)
}

func main() {
	// the go toolchain is built into the executable and must be given a chance to run
	// => avoid side effects in init() as they will occur multiple times during compilation
	if gocompiler.IsRunToolchainRequest() {
		gocompiler.RunToolchain()
		return
	}

	show := false
	shell := false
	trimFlag := false
	showVersion := false
	showCache := false

	help := false
	var arg, filename string
	var programArgs []string
	args := append([]string(nil), os.Args[1:]...)
	for len(args) > 0 {
		arg, args = args[0], args[1:]
		if len(arg) > 2 && strings.HasPrefix(arg, "--") {
			arg = arg[1:]
		}
		if len(arg) > 1 && strings.HasPrefix(arg, "-") {
			switch arg {
			case "-h", "-help":
				help = true
			case "-v", "-version":
				showVersion = true
			case "-c":
				showCache = true
			case "-show":
				// show code
				show = true
			case "-shell":
				shell = true
			case "-trim":
				trimFlag = true
			default:
				errExit(fmt.Sprintf("unknown option %s", arg))
			}
		} else {
			filename, programArgs = arg, args
			help = help || filename == "help"
			showVersion = showVersion || filename == "version"
			break
		}
	}

	// validate flags:
	singleOption := len(os.Args) == 2

	if (trimFlag || showVersion || showCache || help) && !singleOption {
		showUsage()
		errExit(fmt.Sprintf("extra arguments: %s", os.Args[1:]))
	}

	if showVersion {
		fmt.Printf("gorun %s gocompiler %s\n", gorun.GorunVersion(), gocompiler.GoVersion())
		return
	}
	if showCache {
		showCacheUsage()
		return
	}
	if help {
		showUsage()
		return
	}

	if trimFlag {
		c, err := cache.DefaultConfig()
		fmt.Printf("Start trim ...\n")
		if err == nil {
			err = c.TrimNow()
		}
		if err != nil {
			errExit(fmt.Sprintf("%s", err))
		}
		showCacheUsage()
		return
	}

	if filename == "" {
		showUsage()
		errExit("missing file to run")

	}
	var err error
	if filename != "-" {
		filename, err = filepath.Abs(filename)
		if err != nil {
			errExit(fmt.Sprintf("%s", err))
		}
	}
	s := readFileAndStrip(filename)

	c, err := cache.DefaultConfig()
	if err != nil {
		errExit(fmt.Sprintf("cache init failed: %s", err))
	}

	// input must embed everything that affects the computation:
	// = executables, env-vars, commandline
	input := fmt.Sprintf("// gorun: %s\n", gorun.GorunVersion())
	outdir, err := gorun.CompileString(c, s, programArgs, input)

	showBuildInstructions := func() {
		exe, _ := os.Executable()
		fmt.Printf("# how to build:\n")
		fmt.Printf(" cd %s\n", outdir)
		fmt.Printf(" GOCOMPILER_TOOL=go %s build\n", exe)
	}

	if show {
		showBuildInstructions()
	} else if shell {
		showBuildInstructions()

		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		fmt.Printf("# entering shell %s\n", sh)
		cmd := exec.Command(sh) // 2
		cmd.Dir = outdir
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			fmt.Printf("ERROR: %s\n", err)
		}
	} else {
		// normal exec
		if err == nil {
			exefile := filepath.Join(outdir, "main")
			// no lock => only thing protecting the executable is a recent timestamp
			err = gorun.Exec(exefile, args)
			if err != nil {
				errExit(fmt.Sprintf("exec failed: %s", err))
			}
			errExit("exec should not return")
		} else {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
			os.Exit(17)
		}
	}

}

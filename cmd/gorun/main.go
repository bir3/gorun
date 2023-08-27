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
	"path/filepath"
	"strings"

	"github.com/bir3/gocompiler"
	"github.com/bir3/gorun"
	"github.com/bir3/gorun/cache"
)

func GorunVersion() string {
	return "0.4.1"
}

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

  -h    show this help
  -v    show version
  -c    show cache size
  -show show code cache location
  -trim clean cache now

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

	showFlag := false
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
				showFlag = true
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
		fmt.Printf("gorun %s gocompiler %s\n", GorunVersion(), gocompiler.GoVersion())
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

	info := gorun.RunInfo{}
	info.ShowFlag = showFlag
	info.Input = fmt.Sprintf("// gorun: %s\n", GorunVersion())
	err = gorun.ExecString(c, s, programArgs, info)

	if err != nil {
		errExit(fmt.Sprintf("execute failed: %s", err))
	}
}

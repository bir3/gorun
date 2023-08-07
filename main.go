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
	"github.com/bir3/gorun/cache"
)

func GorunVersion() string {
	return "0.4"
}

func readFileAndStrip(filename string) string {
	var s string
	if filename == "-" {
		var out bytes.Buffer
		io.Copy(&out, os.Stdin)
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
	fmt.Fprintf(os.Stderr, "%s\n", msg)
	os.Exit(3)
}

func splitArgs(args []string) ([]string, string, []string) {
	var gorun []string
	var filename string
	var program []string
	for _, arg := range args {
		if arg == "" {
			continue
		}
		if filename == "" {
			if len(arg) > 1 && arg[0] == '-' {
				gorun = append(gorun, arg)
			} else {
				filename = arg
			}
		} else {
			program = append(program, arg)
		}
	}
	return gorun, filename, program
}
func showHelp(help bool) {
	helpStr := `
usage:
  gorun [gorun options] <filename> [program options]

`
	if !help {
		helpStr += "  -h for help"
	} else {
		helpStr += `  -h for help
  -show show cache location`
	}
	helpStr += `
  filename or "-" for stdin; first line can be #! /usr/bin/env gorun

	`
	fmt.Printf("%s\n", strings.TrimSpace(helpStr))
	if help {
		fmt.Println()
		fmt.Printf("gorun version %s\n", GorunVersion())
		fmt.Printf("go compiler version %s\n", gocompiler.GoVersion())
	}
	c, errCache := cache.DefaultConfig()

	if errCache == nil {
		err := c.DeleteExpiredItems()
		if err != nil {
			fmt.Printf("cache trim error: %s\n", err)
		}
		info, err := c.GetInfo()
		if err == nil {
			if help {
				fmt.Printf("cache size is %d MB for %d items in %s\n", info.SizeBytes/1e6, info.Count, info.Dir)
			}
		} else {
			fmt.Printf("cache stat error : %s\n", err)
		}
	} else {
		fmt.Printf("cache init failed: %s\n", errCache)
		os.Exit(4)
	}
	fmt.Println()
}

func main() {

	// the go toolchain is built into the executable and must be given a chance to run
	// => avoid side effects in init() as they will occur multiple times during compilation
	if gocompiler.IsRunToolchainRequest() {
		gocompiler.RunToolchain()
		return
	}

	args, filename, programArgs := splitArgs(os.Args[1:])

	showFlag := false

	help := false
	for len(args) > 0 {
		a0 := args[0]
		args = args[1:]
		if strings.HasPrefix(a0, "-") {
			if a0 == "-h" || a0 == "-help" || a0 == "--help" {
				help = true
			} else {
				switch a0 {
				case "-show":
					// show code
					showFlag = true

				default:
					errExit(fmt.Sprintf("unknown option %s", a0))
				}
			}
		} else {
			errExit("program error")
			break
		}
	}

	if filename == "help" {
		help = true
	}

	if filename == "" || help {
		showHelp(help)
		if help {
			return
		} else {
			errExit("ERROR: missing file to run")
		}
	}
	var err error
	if filename != "-" {
		filename, err = filepath.Abs(filename)
		if err != nil {
			errExit(fmt.Sprintf("%s", err))
		}
	}
	s := readFileAndStrip(filename)
	//fmt.Printf("## s=%s\n", s)

	c, err := cache.DefaultConfig()
	if err != nil {
		fmt.Printf("cache init failed: %s\n", err)
		os.Exit(7)
	}

	info := RunInfo{}
	info.ShowFlag = showFlag
	err = RunString(c, s, programArgs, info)

	if err != nil {
		switch errX := err.(type) {
		case *CompileError:
			fmt.Printf("ERROR: %s\n", errX.Err)
			fmt.Printf("%s", errX.Stdout)
			fmt.Printf("%s", errX.Stderr)
		default:
			fmt.Printf("ERROR: %s", err)
		}
		os.Exit(3)
	}
}

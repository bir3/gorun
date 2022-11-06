// Copyright 2022 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"github.com/bir3/gorun/cache"
	"log"
	"os"
	"path"
	"strings"

	"github.com/bir3/gocompiler"
)

func readFileAndStrip(filename string) string {
	b, err := os.ReadFile(filename)
	if err != nil {
		errExit(fmt.Sprintf("failed to read file %s", filename))
	}
	s := string(b)
	if strings.HasPrefix(s, "#!") {
		i := strings.Index(s, "\n")
		if i < 0 {
			log.Fatal(errors.New("empty file"))
		}
		s = s[i+1:]
	}
	return s
}

func absPath(f string) string {
	if path.IsAbs(f) {
		return path.Clean(f)
	}
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	return path.Join(wd, f)
}

func isHelpArg(s string) bool {
	return s == "-h" || s == "-help" || s == "--help"
}

func errExit(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", msg)
	os.Exit(3)
}

func cacheObject() (*cache.Cache, error) {
	cdir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return cache.CacheInit(cdir), nil
}

func main() {

	// the go toolchain is built into the executable and must be given a chance to run
	// => avoid side effects in init() as they will occur multiple times during compilation
	if gocompiler.IsRunToolchainRequest() {
		gocompiler.RunToolchain()
		return
	}

	args := os.Args[1:]
	c, errCache := cacheObject()

	if len(args) == 0 || len(args) == 1 && isHelpArg(args[0]) {
		helpStr := `
usage:
  gorun <single-file-go-code>  # first line can be #! /usr/bin/env gorun
`
		fmt.Printf("%s\n", strings.TrimSpace(helpStr))
		fmt.Println("\ngo compiler version go1.19.3") // BUG: gocompiler should provide version string

		if errCache == nil {
			err := c.DeleteOld(0)
			if err != nil {
				fmt.Printf("cache trim error: %s\n", err)
			}
			sizeBytes, n, err := c.Stats()
			if err == nil {
				fmt.Printf("cache size is %d MB for %d items in %s\n", sizeBytes/1e6, n, c.Dir())
			} else {
				fmt.Printf("cache stat error : %s\n", err)
			}
		} else {
			fmt.Printf("cache init failed: %s\n", errCache)
			os.Exit(4)
		}
		fmt.Println()
		if len(os.Args) == 1 {
			os.Exit(3)
		}
		return
	}
	cache.Loginit()

	showFlag := false
	remain := make([]string, 0)
	for len(args) > 0 {
		a0 := args[0]
		args = args[1:]
		if strings.HasPrefix(a0, "-") {
			switch a0 {
			case "-show":
				// show code
				showFlag = true
			default:
				errExit(fmt.Sprintf("unknown option %s", a0))
			}
		} else {
			remain = append(remain, a0)
			remain = append(remain, args...)
			break
		}
	}
	args = remain
	if len(args) == 0 {
		errExit("missing file to run")
	}

	filename := absPath(args[0])

	var err error

	s := readFileAndStrip(filename)
	err = cache.RunString2(c, filename, s, args[1:], showFlag)

	if err != nil {
		switch errX := err.(type) {
		case *cache.CompileError:
			fmt.Printf("ERROR: %s\n", errX.Err)
			fmt.Printf("%s", errX.Stdout)
			fmt.Printf("%s", errX.Stderr)
		default:
			fmt.Printf("ERROR: %s", err)
		}
		os.Exit(3)
	}
}

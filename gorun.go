// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gorun

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bir3/gocompiler"
	"github.com/bir3/gorun/cache"
)

type CompileError struct {
	Stdout string
	Stderr string
	Err    error
}

func (c *CompileError) Error() string {
	//return fmt.Sprintf("stdout:\n%s\nstderr:\n%s\nERROR: %s\n", c.Stdout, c.Stderr, c.Err)
	return fmt.Sprintf("%s%s\nERROR: %s\n", c.Stdout, c.Stderr, c.Err)
}

func compile(c *cache.Config, srcfile string, exefile string) error {

	runIf := func(err error, args []string) error {
		if err != nil {
			return err
		}
		cmd, err := gocompiler.Command(os.Environ(), args...)
		if err != nil {
			return fmt.Errorf("failed to create exec.Cmd object - %w", err)
		}
		cmd.Dir = filepath.Dir(exefile)

		var out, outerr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &outerr

		err = cmd.Run()

		if err != nil {
			return &CompileError{out.String(), outerr.String(), err}
		}
		return nil
	}
	var err error

	err = runIf(err, []string{"go", "mod", "init", "main"})

	err = runIf(err, []string{"go", "get"})
	err = runIf(err, []string{"go", "build", "main.go"})
	return err
}

func show(outdir string, inputPart string) {
	exe, _ := os.Executable()
	fmt.Printf("# how to compile manually:\n")
	fmt.Printf(" cd %s\n", outdir)
	fmt.Printf(" GOCOMPILER_TOOL=go %s build\n", exe)
}

type RunInfo struct {
	Input    string
	ShowFlag bool
}

func ExecString(c *cache.Config, goCode string, args []string, info RunInfo) error {

	// must add everything that affects the computation:
	// = input file, executables, env-vars, commandline
	//
	input := info.Input

	input += fmt.Sprintf("// gocompiler: %s\n", gocompiler.GoVersion())
	input += fmt.Sprintf("// env.CGO_ENABLED: %s\n", os.Getenv("CGO_ENABLED"))
	input += "//\n"
	input += fmt.Sprintf("%s\n", goCode)

	showDone := info.ShowFlag
	show := func(outdir string) {
		if showDone {
			show(outdir, input[0:strings.Index(input, "\n//\n")])
			showDone = false
		}
	}

	// TODO: if periodic cleanup time has arrived
	// take exclusive lock on Lookup
	// but only execute cleanup if create() called
	// => and exclude current item from deletion
	// => we can hope to execute delete with zero time impact
	// for the user
	// downside: we will hold an exclusive locks during this time
	// hmm, can we execute cache hit without lock to minimize impact ?
	createCalled := false
	outdir, err := c.Lookup(input, func(outdir string) error {

		create := func() error {

			createCalled = true
			gofile := filepath.Join(outdir, "main.go")
			exefile := filepath.Join(outdir, "main")

			// write main.go
			err := os.WriteFile(gofile, []byte(goCode), 0666)
			if err != nil {
				return fmt.Errorf("failed to write %s - %w", gofile, err)
			}

			err = compile(c, gofile, exefile)

			return err
		}
		err := create()
		show(outdir) // outdir only here if error during compile
		return err
	})

	if createCalled {
		// create item called (e.g. no cached item found)
		// => we are already on a slow path
		// => check if cache trim should occur
		c.TrimPeriodically()
	}

	if err != nil {
		return err
	}

	show(outdir) // outdir only here if found in cache

	exefile := filepath.Join(outdir, "main")
	// no lock => only thing protecting the executable is a recent timestamp

	if !info.ShowFlag {
		return sysExec(exefile, args)
	}
	return nil
}

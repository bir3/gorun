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

func GorunVersion() string {
	return "0.6.0"
}

type CompileError struct {
	Stdout string
	Stderr string
	Err    error
}

func (c *CompileError) Error() string {
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
			var err error = &CompileError{out.String(), outerr.String(), err}
			cmdline := strings.Join(args, " ")
			return fmt.Errorf("# cd %s\n# %s\n%w", cmd.Dir, cmdline, err)
		}
		return nil
	}
	var err error

	err = runIf(err, []string{"go", "mod", "init", "main"})

	err = runIf(err, []string{"go", "get"})
	err = runIf(err, []string{"go", "build", "main.go"})
	return err
}

func CompileString(c *cache.Config, goCode string, args []string, input string) (string, error) {

	// must add everything that affects the computation:
	// = input file, executables, env-vars, commandline
	//

	input += fmt.Sprintf("// gocompiler: %s\n", gocompiler.GoVersion())
	input += fmt.Sprintf("// gorun: %s\n", GorunVersion())
	input += fmt.Sprintf("// env.CGO_ENABLED: %s\n", os.Getenv("CGO_ENABLED"))
	input += "//\n"
	input += fmt.Sprintf("%s\n", goCode)

	incompleteOutdir := ""

	createCalled := false
	outdir, err := c.Lookup(input, func(outdir string) error {

		create := func() error {

			createCalled = true
			gofile := filepath.Join(outdir, "main.go")
			exefile := filepath.Join(outdir, "main")

			err := os.WriteFile(gofile, []byte(goCode), 0666)
			if err != nil {
				return fmt.Errorf("failed to write %s - %w", gofile, err)
			}

			err = compile(c, gofile, exefile)

			return err
		}
		err := create()
		incompleteOutdir = outdir // outdir only here if error during compile
		return err
	})

	if outdir == "" {
		outdir = incompleteOutdir
	}

	if err == nil && createCalled {
		// create called = no cached item found
		// => we are already on a slow path
		// => check if cache trim should occur
		c.TrimPeriodically() // NOTE: error ignored - should be visible on request
	}

	return outdir, err

}

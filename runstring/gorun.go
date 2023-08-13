package runstring

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
	return fmt.Sprintf("stdout:\n%s\nstderr:%s\nerr: %s", c.Stdout, c.Stderr, c.Err)
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
	fmt.Printf("// cd %s\n", outdir)
	fmt.Printf("// GOCOMPILER_TOOL=go %s build\n", exe)
	fmt.Printf("%s\n", inputPart)
	buf, err := os.ReadFile(fmt.Sprintf("%s/go.mod", outdir))
	if err == nil {
		fmt.Printf("// go.mod:\n")
		fmt.Printf("%s\n", string(buf))
	}
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

	outdir, err := c.Lookup(input, func(outdir string) error {

		create := func() error {
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

package run2

import (
	"bytes"
	"fmt"
	"os"
	"path"
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

//func (e *CompileError) Error() string { return e.Err.Error() }

//func (e *CompileError) Unwrap() error { return e.Err }

func compile(srcfile string, exefile string, s string) error {

	run := func(args []string) error {
		cmd, err := gocompiler.Command(os.Environ(), args...)
		if err != nil {
			return fmt.Errorf("failed to create exec.Cmd object - %w", err)
		}
		cmd.Dir = filepath.Dir(exefile)

		//cmd.Env = append(cmd.Env, fmt.Sprintf("GOMODCACHE=%s/go-mod", cmd.Dir))
		//cmd.Env = append(cmd.Env, fmt.Sprintf("GOCACHE=%s/go-build", cmd.Dir))
		var out bytes.Buffer
		var outerr bytes.Buffer

		cmd.Stdout = &out
		cmd.Stderr = &outerr
		err = cmd.Run()

		if err != nil {
			return &CompileError{out.String(), outerr.String(), err}
		}
		return nil
	}
	args := []string{"go", "get"}
	err := run(args)
	if err != nil {
		return err
	}
	args = []string{"go", "build", "main.go"}
	err = run(args)
	if err != nil {
		return err
	}
	return err
}

const goModString = `module main

go 1.18

// hash $hash
// file $file

`

func show(outdir string) {
	exe, _ := os.Executable()
	fmt.Printf("// cd %s\n", outdir)
	fmt.Printf("// GOCOMPILER_TOOL=go %s build\n", exe)
}

func RunString2(c *cache.Config, srcpath string, s string, args []string, showFlag bool) error {
	// simple cache: only store one copy per unique filepath
	srcpath = path.Clean(srcpath)

	// TODO: add everything that affects computation:
	// = input file, executables, env-vars, commandline
	input := fmt.Sprintf("%s\n", s)

	outdir, err := c.Lookup(input, func(outdir string) error {
		if showFlag {
			show(outdir)
			showFlag = false
		}
		modfile := filepath.Join(outdir, "go.mod")
		gofile := filepath.Join(outdir, "main.go")
		exefile := filepath.Join(outdir, "main")

		hash := "xx" // hashString(goRunVersion + "#" + s) // if options, need them here

		// write go.mod
		modstr := goModString
		modstr = strings.ReplaceAll(modstr, "$hash", hash)
		modstr = strings.ReplaceAll(modstr, "$file", srcpath)

		err := os.WriteFile(modfile, []byte(modstr), 0666)

		if err != nil {
			return fmt.Errorf("failed to create file %s - %w", modfile, err)
		}

		// write main.go
		err = os.WriteFile(gofile, []byte(s), 0666)
		if err != nil {
			return fmt.Errorf("failed to write %s - %w", gofile, err)
		}
		err = compile(gofile, exefile, s)

		return err
	})

	if err != nil {
		return err
	}

	if showFlag {
		show(outdir)
		showFlag = false
	}

	exefile := filepath.Join(outdir, "main")
	// no lock => only thing protecting the executable is a recent timestamp

	if showFlag {
		mainfile := srcpath
		fmt.Printf("// %s\n", srcpath)
		fmt.Printf("// -> %s\n", mainfile)
		fmt.Printf("//\n")
		if strings.HasSuffix(s, "\n") {
			fmt.Print(s)
		} else {
			fmt.Println(s)
		}
	} else {
		return sysExec(exefile, args)
	}
	return nil
}

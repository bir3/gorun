package cache

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bir3/gocompiler"
)

type CompileError struct {
	Stdout string
	Stderr string
	Err    error
}

/*
	func (c *CompileError) Error() string {
		return fmt.Sprintf("stdout:\n%s\nstderr:%s\nerr: %s", c.Stdout, c.Stderr, c.Err)
	}
*/
func (e *CompileError) Error() string { return e.Err.Error() }

//func (e *CompileError) Unwrap() error { return e.Err }

func writeFileAndCompile(srcfile string, exefile string, s string) error {

	err := os.WriteFile(srcfile, []byte(s), 0666)
	if err != nil {
		return fmt.Errorf("failed to write %s - %w", srcfile, err)
	}

	result, err := gocompiler.Run("go", "build", "-o", exefile, srcfile)
	if err != nil {
		return &CompileError{result.Stdout, result.Stderr, err}
	}

	return nil
}

func buildexe(c *Config, srcpath, gofile string, modfile string, exefile string, s string) error {
	goRunVersion := "x"                        // FIXME
	hash := hashString(goRunVersion + "#" + s) // if options, need them here

	err := writeModfile(modfile, srcpath, hash) // if exit after this point, modfile will say executable may exist
	if err != nil {
		return fmt.Errorf("failed to create file %s - %w", modfile, err)
	}

	//logmsg("compile: start")
	err = writeFileAndCompile(gofile, exefile, s)
	if err != nil {
		switch err.(type) {
		case *CompileError:
			return err
		default:
			return fmt.Errorf("failed to build exe %s - %w", exefile, err)
		}

	}
	//logmsg("compile: done")

	// c.DeleteOld(maxDeleteDuration)

	return nil
}

func writeModfile(modfile string, filepath string, hash string) error {
	goModString := `module gorun

go 1.18

// hash $hash
// file $file

`

	goModString = strings.ReplaceAll(goModString, "$hash", hash)
	goModString = strings.ReplaceAll(goModString, "$file", filepath)

	err := os.WriteFile(modfile, []byte(goModString), 0666)

	return err
}

func RunString2(c *Config, srcpath string, s string, args []string, showFlag bool) error {
	// simple cache: only store one copy per unique filepath
	srcpath = path.Clean(srcpath)

	// TODO: add everything that affects computation:
	// = input file, executables, env-vars, commandline
	input := fmt.Sprintf("%s\n", s)

	outdir, err := c.Lookup(input, func(outdir string) error {
		modfile := filepath.Join(outdir, "go.mod")
		exefile := filepath.Join(outdir, "main")
		gofile := filepath.Join(outdir, "main.go")

		err := buildexe(c, srcpath, gofile, modfile, exefile, s)
		return err
	})

	if err != nil {
		return err
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

func sysExec(exefile string, args []string) error {
	args2 := []string{exefile}
	args2 = append(args2, args...)
	err := syscall.Exec(exefile, args2, os.Environ())
	if err != nil {
		return fmt.Errorf("syscall.Exec failed for %s - %w", exefile, err)
	}
	return nil // unreachable - exec should not return
}

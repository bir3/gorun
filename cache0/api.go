// Copyright 2022 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"fmt"
	"io"
	"path"
	"strings"

	"os"

	"github.com/bir3/gocompiler"
)

var goRunVersion string = "4" // readFile("main.go") // "1.1"

type CompileError struct {
	Stdout string
	Stderr string
	Err    error
}

func (e *CompileError) Error() string { return e.Err.Error() }

func (e *CompileError) Unwrap() error { return e.Err }

func RunString(filepath string, s string, args []string) error {
	cdir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("cache init failed - %w", err)
	}
	c := CacheInit(cdir)
	show := false

	return RunString2(c, filepath, s, args, show)
}

func (c *Cache) filepath(srcpath string, filename string) string {
	h := hashString(srcpath)
	return path.Join(c.dir, h[0:2], h, filename)
}
func (c *Cache) createdir(srcpath string) {
	d1 := path.Dir(c.filepath(srcpath, "x"))
	d2 := path.Dir(d1)
	ensureDir(d2)
	ensureDir(d1)
}
func RunString2(c *Cache, srcpath string, s string, args []string, showFlag bool) error {
	// simple cache: only store one copy per unique filepath
	srcpath = path.Clean(srcpath)

	c.createdir(srcpath)
	dbfile := c.filepath(srcpath, "go.mod")

	exefile := c.filepath(srcpath, "main")
	srcfile := c.filepath(srcpath, "main.go")

	f, err := openFileAndLock(dbfile)
	if err != nil {
		return err
	}
	db, err := readFileObject(f)
	if err == nil {
		err = buildexe(c, db, f, srcpath, srcfile, dbfile, exefile, s)
	}
	err2 := unlockAndClose(f)
	if err != nil {
		return err
	}
	if err2 != nil {
		return err2
	}

	// no lock => only thing protecting the executable is a recent timestamp

	if showFlag {
		mainfile := srcfile
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

func buildexe(c *Cache, db string, f *os.File, filepath string, srcfile string, modfile string, exefile string, s string) error {
	hash := hashString(goRunVersion + "#" + s) // if options, need them here

	found, err := pathExists(exefile)
	if err != nil {
		return fmt.Errorf("pathExists failed for %s - error %w", exefile, err)
	}

	if found && strings.Contains(db, hash) {
		// go.mod says executable may exist

		// touch exefile mtime => prevent delete for some grace period
		err = c.used(exefile)
		if err != nil {
			return fmt.Errorf("failed to update timestamp for %s - error %w", exefile, err)
		}

	} else {
		// delete old executable if any
		if found {
			err = os.Remove(exefile)
			if err != nil {
				return fmt.Errorf("failed to delete old executable %s - error %w", exefile, err)
			}
			found = false
		}
	}

	if !found {
		var exefound bool
		exefound, err = pathExists(exefile)
		if err != nil {
			return fmt.Errorf("pathExists failed for %s - error %w", exefile, err)
		}
		if exefound {
			return fmt.Errorf("program error - found executable %s", exefile)
		}
		err = writeModfile(f, filepath, hash) // if exit after this point, modfile will say executable may exist
		if err != nil {
			return fmt.Errorf("failed to create file %s - %w", modfile, err)
		}

		logmsg("compile: start")
		err = writeFileAndCompile(srcfile, exefile, s)
		if err != nil {
			switch err.(type) {
			case *CompileError:
				return err
			default:
				return fmt.Errorf("failed to build exe %s - %w", exefile, err)
			}

		}
		logmsg("compile: done")
		c.DeleteOld(maxDeleteDuration)
	}
	return nil
}

func writeModfile(fmodfile *os.File, filepath string, hash string) error {
	goModString := `module gorun

go 1.18

// hash $hash
// file $file
// $magic
`

	goModString = strings.ReplaceAll(goModString, "$hash", hash)
	goModString = strings.ReplaceAll(goModString, "$file", filepath)
	goModString = strings.ReplaceAll(goModString, "$magic", GORUN_MAGIC_STRING)

	_, err := fmodfile.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	err = fmodfile.Truncate(0)
	if err != nil {
		return err
	}
	_, err = fmodfile.WriteString(goModString)
	return err
}

func deleteIfExists(filename string) error {
	found, err := pathExists(filename)
	if err == nil && found {
		err = os.Remove(filename)
	}
	return err
}

func writeFileAndCompile(srcfile string, exefile string, s string) error {

	err := os.WriteFile(srcfile, []byte(s), 0666)
	if err != nil {
		return fmt.Errorf("failed to write %s - %w", srcfile, err)
	}

	exefile_tmp := exefile + "-tmp"
	err = deleteIfExists(exefile_tmp)
	if err != nil {
		return fmt.Errorf("failed to delete %s - %w", exefile_tmp, err)
	}

	result, err := gocompiler.Run("go", "build", "-o", exefile_tmp, srcfile)
	if err != nil {
		return &CompileError{result.Stdout, result.Stderr, err}
	}
	err = os.Rename(exefile_tmp, exefile)
	if err != nil {
		return fmt.Errorf("failed to rename %s - %w", exefile_tmp, err)
	}
	return nil
}

// Copyright 2022 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"gorun/filelock"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// use Sub().Abs() only in go 1.19
func timeDiff(t1 time.Time, t2 time.Time) time.Duration {
	if t1.After(t2) {
		return t1.Sub(t2)
	} else {
		return t2.Sub(t1)
	}
}

type Log struct {
	f  *os.File
	t0 float64
}

var logger Log

func Loginit() {
	e := strings.Split(strings.TrimSpace(os.Getenv("GORUN_LOGFILE")), ":")
	if len(e[0]) > 0 {
		if len(e) > 1 {
			var err error
			logger.t0, err = strconv.ParseFloat(e[1], 64)
			if err != nil {
				logger.t0 = 0
			}
		}
		logfile := e[0]
		logger.f, _ = os.OpenFile(logfile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		logmsg("log start")
	}
}

func logmsg(msg string) {

	if logger.f != nil && len(msg) > 0 {

		t_ms := time.Now().UnixMilli()
		if logger.t0 > 0 {
			t_ms = t_ms - int64(logger.t0*1000) // utc
		}
		if msg[len(msg)-1:] != "\n" {

			_, err := logger.f.WriteString(fmt.Sprintf("%d.%03d ", t_ms/1000, t_ms%1000) + msg + "\n")
			if err != nil {
				panic(err) // allow panic since logging is by default off
			}

		} else {
			logger.f.WriteString(msg)
		}
	}
}

func openFileAndLock(ifile string) (*os.File, error) {
	f, err := os.OpenFile(ifile, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open/create file %s - %w", ifile, err)

	}
	err = filelock.Lock(f)
	if err != nil {
		return nil, fmt.Errorf("failed to lock file %s - %w", ifile, err)
	}
	logmsg("lock")
	return f, nil
}

func unlockAndClose(f *os.File) error {
	err := filelock.Unlock(f)
	if err != nil {
		return fmt.Errorf("failed to unlock file %s - %w", f.Name(), err)
	}
	logmsg("unlock")
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close file %s - %w", f.Name(), err)
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

func filepathModTime(filepath string) (time.Time, error) {
	stat, err := os.Stat(filepath)
	return stat.ModTime(), err
}

func ensureDir(dir string) {
	// assume many will race here
	_, err := os.Stat(dir)
	if err != nil {
		err = os.Mkdir(dir, 0755)
		if err != nil {
			logmsg(fmt.Sprintf("race mkdir: %s", err))
		}
	}
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func readFileObject(f io.Reader) (string, error) {
	buf := new(strings.Builder)
	_, err := io.Copy(buf, f)
	if err != nil {
		return "", err
	}

	s := buf.String()
	return s, nil

}

package cache

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/bir3/gorun/filelock"
)

type LockType int

const (
	SharedLock    LockType = 64
	ExclusiveLock LockType = 128
)

func updateDatafile(datafile string, update func(old string, writeString func(new string) error) error) error {

	if !utf8.Valid([]byte(datafile)) || strings.Contains(datafile, "\x00") {
		return fmt.Errorf("bad datafile characters: %q", datafile)
	}

	file, err := os.OpenFile(datafile, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return fmt.Errorf("open file %q failed - %w", datafile, err)
	}
	writeString := func(s string) error {
		_, err = file.Seek(0, io.SeekStart)
		if err != nil {
			return fmt.Errorf("seek 0 failed for file %q - %w", datafile, err)
		}
		_, err := file.WriteString(s)
		if err != nil {
			return fmt.Errorf("WriteString failed for file %q - %w", datafile, err)
		}

		return nil
	}

	var b bytes.Buffer
	buf := bufio.NewWriter(&b)
	_, err = io.Copy(buf, file)
	if err != nil {
		return fmt.Errorf("read file %q failed - %w", datafile, err)
	}
	old := b.String()
	err = update(old, writeString)

	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func OnceMultiprocess(lockfile string, datafile string, userFunc func() error) error {
	const FinalString = "done\n"
	const RunString = "f..\n"
	if len(RunString) > len(FinalString) {
		return fmt.Errorf("program error")
	}
	updateContent := func(old string, writeString func(new string) error) error {
		if old != FinalString {
			// extra write to test that commit is possible
			// note: string must be shorter than our final string
			err := writeString(RunString)
			if err == nil {
				err = userFunc()
				if err != nil {
					return fmt.Errorf("userFunc failed - %w", err)
				}
				err = writeString(FinalString)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	f2 := func() error {
		return updateDatafile(datafile, updateContent)
	}
	return Lockedfile(lockfile, ExclusiveLock, f2)
}

func UpdateMultiprocess(lockfile string, datafile string, updateContent func(old string, writeString func(new string) error) error) error {
	f2 := func() error {
		return updateDatafile(datafile, updateContent)
	}
	return Lockedfile(lockfile, ExclusiveLock, f2)
}

func Lockedfile(lockfile string, lockType LockType, f func() error) error {

	if !utf8.Valid([]byte(lockfile)) || strings.Contains(lockfile, "\x00") {
		return fmt.Errorf("bad lockfile characters: %q", lockfile)
	}
	// separate datafile is used to ensure that all file operations
	// complete before lock is released (as opposed to storing the data in the lockfile)

	file, err := os.OpenFile(lockfile, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return fmt.Errorf("failed to open/create file %s - %w", lockfile, err)
	}
	if lockType == SharedLock {
		err = filelock.RLock(file)
	} else {
		err = filelock.Lock(file)
	}

	if err != nil {
		file.Close()
		return fmt.Errorf("failed to lock file %s - %w", lockfile, err)
	}
	//log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	//log.Printf("holding lock on file %s\n", lockfile)
	errorOut := f()

	errUnlock := filelock.Unlock(file)
	errClose := file.Close()

	if errorOut == nil && errUnlock != nil {
		errorOut = fmt.Errorf("unlock failed: %w", errUnlock)
	}
	//log.Printf("released lock on file %s\n", lockfile)
	if errorOut == nil && errClose != nil {
		errorOut = fmt.Errorf("close failed: %w", errClose)
	}

	return errorOut
}

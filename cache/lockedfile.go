// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/bir3/gocompiler/extra/filelock"
)

type LockType int

const (
	SHARED_LOCK    LockType = 64
	EXCLUSIVE_LOCK LockType = 128
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
	_, err = io.Copy(&b, file)
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

func UpdateMultiprocess(lockfile string, lockType LockType, datafile string, updateContent func(old string, writeString func(new string) error) error) error {
	// easier to understand api if lock type is explicit even if only one value allowed
	if lockType != EXCLUSIVE_LOCK {
		return fmt.Errorf("must specify ExclusiveLock")
	}
	f2 := func() error {
		return updateDatafile(datafile, updateContent)
	}
	return Lockedfile(lockfile, EXCLUSIVE_LOCK, f2)
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
	if lockType == SHARED_LOCK {
		err = filelock.RLock(file)
	} else {
		err = filelock.Lock(file)
	}

	if err != nil {
		file.Close()
		return fmt.Errorf("failed to lock file %s - %w", lockfile, err)
	}
	errorOut := f()

	errUnlock := filelock.Unlock(file)
	errClose := file.Close()

	if errorOut == nil && errUnlock != nil {
		errorOut = fmt.Errorf("unlock failed: %w", errUnlock)
	}

	if errorOut == nil && errClose != nil {
		errorOut = fmt.Errorf("close failed: %w", errClose)
	}

	return errorOut
}

// Copyright 2022 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const GORUN_MAGIC_STRING = "https://github.com/bir3/gorun/CACHE_ITEM_MAGIC_STRING"

type Cache struct {
	dir string
	now func() time.Time
}

const (
	maxDeleteDuration = 500 * time.Millisecond
	mtimeInterval     = 1 * time.Hour
	trimInterval      = 24 * time.Hour
	maxFileAge        = 5 * 24 * time.Hour
)

func CacheInit(cacheDir string) *Cache {
	var c Cache
	c.now = time.Now // style from Go toolchain: src/cmd/go/internal/cache/cache.go

	ensureDir(cacheDir)

	dir2 := path.Join(cacheDir, "gorun")
	c.dir = dir2
	ensureDir(c.dir)

	return &c
}

func (c *Cache) Dir() string {
	return c.dir
}

func (c *Cache) used(file string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	if timeDiff(c.now(), info.ModTime()) < mtimeInterval {
		return nil
	}
	return os.Chtimes(file, c.now(), c.now())
}

func (c *Cache) Stats() (int64, int, error) {
	var size int64 = 0
	var n int = 0
	e := filepath.WalkDir(c.dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "main" {
			info, err := d.Info()
			if err == nil {
				size += info.Size()
				n += 1
			}
		}
		return nil
	})
	if e != nil {
		return 0, 0, e
	}
	return size, n, nil
}

func (c *Cache) isOld(filepath string, maxAge time.Duration) (bool, error) {
	mtime, err := filepathModTime(filepath)
	if err != nil {
		return false, err
	}
	return timeDiff(c.now(), mtime) > maxAge, nil
}

func (c *Cache) shouldTrim() (string, error) {
	trimfilepath := path.Join(c.dir, "trim.txt")

	f, err := os.OpenFile(trimfilepath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err == nil {
		err = f.Close()
		if err != nil {
			return "", err
		}
	}
	// potential race so assume file exists by now
	old, err := c.isOld(trimfilepath, trimInterval)
	if err != nil {
		return "", err
	}
	if !old {
		return "", nil
	}
	// multiple processes can race here
	// - try to minimize those that get through without
	trimlockfile := path.Join(c.dir, "trim-lock.txt")
	f, err = os.OpenFile(trimlockfile, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		// we lost !
		old, err = c.isOld(trimlockfile, mtimeInterval)
		if err == nil && old {
			// cleanup - potential race so ignore error
			os.Remove(trimlockfile)
		}
		return "", nil
	}
	err = c.used(trimfilepath)
	err2 := f.Close()
	if err != nil {
		return "", err
	}
	if err2 != nil {
		return "", err2
	}
	return trimlockfile, nil
}

func (c *Cache) DeleteOld(maxDuration time.Duration) error {
	trimlockfile, err := c.shouldTrim()
	if err != nil {
		return err
	}
	if len(trimlockfile) == 0 {
		return nil
	}
	t0 := c.now()
	n := 0
	errWalk := filepath.WalkDir(c.dir, func(filepath string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (d.Name() == "main" || d.Name() == "main-tmp") {
			n = n + 1
			old, err := c.isOld(filepath, maxFileAge)
			if err == nil && old {
				// file is old enough to be candidate for deletion
				c.tryDelete(filepath)
			}
		}
		if err == nil {
			dt := timeDiff(t0, c.now())
			if maxDuration > 0 && dt > maxDuration {
				msg := fmt.Sprintf("slow DeleteOld : exiting after %d and %d items", dt, n)
				logmsg(msg)
				return fmt.Errorf("%s", msg)
			}
		}
		return err
	})
	os.Remove(trimlockfile)
	return errWalk
}

func (c *Cache) tryDelete(filepath string) {
	if path.IsAbs(filepath) {
		modfile := path.Join(path.Dir(filepath), "go.mod")
		b, err := os.ReadFile(modfile)
		if err != nil {
			return
		}
		s := string(b)
		if strings.Contains(s, GORUN_MAGIC_STRING) {
			f, err := openFileAndLock(modfile)
			if err != nil {
				return
			}
			// still old ?
			old, err := c.isOld(filepath, maxFileAge)
			if err == nil && old {
				err = os.Remove(filepath)
				if err == nil {
					logmsg(fmt.Sprintf("cache item %s deleted", filepath))
				}
			}
			unlockAndClose(f)
		}
	}

}

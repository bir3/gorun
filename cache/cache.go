// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bir3/gocompiler/extra"
)

func jsonString(m map[string]string) (string, error) {

	buf, err := json.Marshal(m)
	var out bytes.Buffer
	if err == nil {
		err = json.Indent(&out, buf, "", "  ")
	}
	if err != nil {
		return "", err
	}
	final := out.String() + "\n"
	return final, nil
}

type Stat struct {
	Count     int
	SizeBytes int64
	Dir       string
}

func (config *Config) GetInfo() (Stat, error) {
	info := Stat{}
	for part := 0; part < 256; part++ {
		config.GetPartInfo(&info, part)
	}
	info.Dir = config.dir
	return info, nil
}

func (config *Config) GetPartInfo(stat *Stat, part int) {
	dir := config.partPrefix(part)

	e := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				stat.SizeBytes += info.Size()
				stat.Count += 1
			}
		}
		return nil
	})
	if e != nil {
		return
	}
}

func lockfile2datafile(lockfile string) string {
	return filepath.Join(filepath.Dir(lockfile), "info")
}

type Item struct {
	objdir          string
	refreshTime     int64
	refreshTimeNano int // only to support faster tests
}

func (obj *Item) refresh() {
	t := time.Now()
	obj.refreshTime = t.Unix()
	obj.refreshTimeNano = t.Nanosecond()
}
func (obj *Item) age() time.Duration {
	t2 := time.Unix(obj.refreshTime, int64(obj.refreshTimeNano))
	dt := time.Since(t2)
	return dt.Abs()
}

func item2str(obj Item) string {
	return fmt.Sprintf("%s %d %d\n", obj.objdir, obj.refreshTime, obj.refreshTimeNano)
}

func str2item(s string) (Item, error) {
	// format: objdir + " " + unixtime + "\n"
	// extra content after newline is allowed and ignored

	k := strings.Index(s, "\n")
	if k < 0 {
		return Item{"", 0, 0}, fmt.Errorf("parse, missing newline")
	}
	s = s[0:k]
	e := strings.Fields(s)
	if len(e) != 3 {
		return Item{"", 0, 0}, fmt.Errorf("parse, not three fields: %q", e)
	}
	var err error
	i, err := strconv.ParseInt(e[1], 10, 64)
	if err != nil {
		return Item{"", 0, 0}, fmt.Errorf("parse int failed: %q - %w", e[1], err)
	}
	iNano, err := strconv.Atoi(e[2])
	if err != nil {
		return Item{"", 0, 0}, fmt.Errorf("parse int failed: %q - %w", e[2], err)
	}
	return Item{e[0], i, iNano}, nil

}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	// always 64 characters, even with leading zero
	return fmt.Sprintf("%x", sum)
}

func randomHash() string {
	// assume minimum go1.20 => no need for rand.Seed(time.Now().UnixNano())
	uid := fmt.Sprintf("%d", rand.Int63())
	return hashString(uid)
}

// input should be a string that represents a complete description of a
// repeatable computation (command line equivalent, environment variables,
// input file contents, executable contents).
// returns outdir
func Lookup(input string, create func(outDir string) error) (string, error) {
	config, err := DefaultConfig()
	if err != nil {
		return "", err
	}
	const useCache = true
	return config.Lookup2(input, create, useCache)
}
func (config *Config) Lookup(input string, create func(outDir string) error) (string, error) {
	const useCache = true
	return config.Lookup2(input, create, useCache)
}

func (config *Config) Lookup2(input string, userCreate func(outDir string) error, useCache bool) (string, error) {
	// NOTE: useCache ignored - if used, must not delete other outdir's that may still be in use

	hs := hashString(input)
	pair := config.itemLock(hs)
	lockfile := pair.lockfile
	datafile := pair.datafile

	err := extra.MkdirAllRace(pair.dir(), 0777)
	if err != nil {
		return "/invalid/outdir/1", fmt.Errorf("failed to create prefix dir %q - %w", pair.dir(), err)
	}

	var outdir string
	updateContent := func(old string, writeString func(new string) error) error {

		if old == "" {
			// object not created yet
			outdir = filepath.Join(pair.dir(), randomHash()[0:8]) // 8 chars = 32 bits
			err := os.Mkdir(outdir, 0777)
			if err != nil {
				return fmt.Errorf("outdir %q already exists - program error", outdir)
			}
			err = userCreate(outdir)
			if err != nil {
				// keep folder so user can debug problem
				return err
			}
			var obj Item
			obj.objdir = outdir
			obj.refresh()
			//
			// careful: next line is commit
			err = writeString(item2str(obj))
			if err != nil {
				// keep folder so user can debug problem
				return err
			}
			return nil
		} else {
			obj, err := str2item(old)
			if err != nil {
				return fmt.Errorf("cache corruption in file %q - %w", datafile, err)
			}

			outdir = obj.objdir
			age := obj.age()
			if age > config.maxAge/10 {
				obj.refresh()
			}
			err = writeString(item2str(obj))
			if err != nil {
				return fmt.Errorf("cache refresh failed for file %q - %w", datafile, err)
			}

		}
		return nil
	}
	withPartLock := func() error {
		return UpdateMultiprocess(lockfile, EXCLUSIVE_LOCK, datafile, updateContent)
	}
	withGlobalLock := func() error {
		return Lockedfile(config.partLock(hs).lockfile, SHARED_LOCK, withPartLock)
	}
	err = Lockedfile(config.globalLock().lockfile, SHARED_LOCK, withGlobalLock)
	if err != nil {
		return "/invalid/outdir/2", err
	}
	return outdir, nil
}

func ensureDir(dir string) error {
	fileinfo, err := os.Stat(dir)
	if err == nil && fileinfo.IsDir() {
		return nil
	}
	if err != nil {
		return os.Mkdir(dir, 0777)
	}
	return nil
}

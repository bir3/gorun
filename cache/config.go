// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Config struct {
	dir string // no trailing slashes

	maxAge time.Duration // safe to delete objects older than this
	re1    *regexp.Regexp
	re2    *regexp.Regexp
}
type Lockpair struct {
	lockfile string
	datafile string
}

func (pair *Lockpair) dir() string {
	return filepath.Dir(pair.lockfile)
}
func NewLockPair(dir, lockfile, datafile string) Lockpair {
	panicIf(filepath.Base(lockfile) != lockfile)
	panicIf(filepath.Base(datafile) != datafile)
	return Lockpair{filepath.Join(dir, lockfile), filepath.Join(dir, datafile)}
}

func panicIf(doPanic bool) {
	if doPanic {
		panic("program error")
	}
}

func mkdirAllRace(dir string) error {
	// safe for many processes to run concurrently
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("program error: folder is not absolute path: %s", dir)
	}
	missing, err := missingFolders(dir, []string{})
	if err != nil {
		return err
	}
	for _, d2 := range missing {
		os.Mkdir(d2, 0777) // ignore error as we may race
	}

	// at the end, we want a folder to exist
	// - no matter who created it:
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("failed to create folder %s - %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a folder %s", dir)
	}

	return nil
}

func missingFolders(dir string, missing []string) ([]string, error) {
	for {
		info, err := os.Stat(dir)
		if err == nil {
			if info.IsDir() {
				return missing, nil
			}
			return []string{}, fmt.Errorf("not a folder: %s", dir)
		}
		missing = append([]string{dir}, missing...) // prepend => reverse order
		d2 := filepath.Dir(dir)
		if d2 == dir {
			break
		}
		dir = d2
	}
	return []string{}, fmt.Errorf("program error at folder: %s", dir)
}

func (config *Config) Dir() string {
	return config.dir
}
func (config *Config) globalLock() Lockpair {
	return NewLockPair(config.dir, "config.lock", "config.json")
}
func (config *Config) trimLock() Lockpair {
	return NewLockPair(config.dir, "trim.lock", "trim.txt")
}

func (config *Config) partLock(hash string) Lockpair {
	return NewLockPair(config.partPrefixFromHash(hash), "lockfile", "info")
}
func (config *Config) itemLock(hash string) Lockpair {
	dir := filepath.Join(config.partPrefixFromHash(hash), hash[0:40]) // use 40x4 = 160 bits
	return NewLockPair(dir, "lockfile", "info")
}

func (config *Config) prefix() string {
	return filepath.Join(config.dir, "data")
}
func (config *Config) partPrefix(part int) string {
	if part < 0 || part > 255 {
		panic(fmt.Sprintf("bad part %d", part))
	}
	hash2 := fmt.Sprintf("%02x-t", part)
	return filepath.Join(config.prefix(), hash2)
}
func (config *Config) partPrefixFromHash(hash string) string {
	i, err := strconv.ParseInt(hash[0:2], 16, 32)
	if err != nil {
		panic(err)
	}
	return config.partPrefix(int(i))
}

func NewConfig(dir string, maxAge time.Duration) (*Config, error) {

	if maxAge < 10*time.Second {
		return nil, fmt.Errorf("maxAge minimum is 10 seconds")
	}
	return newConfig(dir, maxAge)
}

func writeREADME(dir string) {
	s := `
cache folder maintained by https://github.com/bir3/gorun
	`
	s = strings.TrimSpace(s) + "\n"
	os.WriteFile(filepath.Join(dir, "README"), []byte(s), 0666)
}

func newConfig(dir string, maxAge time.Duration) (*Config, error) {
	if maxAge < 10*time.Millisecond {
		return nil, fmt.Errorf("internal maxAge minimum is 10 milliseconds")
	}

	if !utf8.Valid([]byte(dir)) {
		return nil, fmt.Errorf("config dir is not utf8: %q", dir)
	}
	dir = filepath.Clean(dir) // only ends in trailing slash if root /
	if !filepath.IsAbs(dir) || strings.Contains(dir, "\x00") {
		return nil, fmt.Errorf("bad characters in config dir : %q", dir)
	}

	config := &Config{dir, maxAge, regexp.MustCompile(`^[a-z0-9]{2}-t$`), regexp.MustCompile(`^[a-z0-9]{40}$`)}

	mkdirAllRace(dir)

	m := make(map[string]string)

	updateContent := func(old string, writeString func(new string) error) error {
		m["maxAge"] = maxAge.String()
		m["#info-maxAge"] = "valid units are h, m and s"

		final, err := jsonString(m)
		if err != nil {
			return err
		}

		if old == "" { // = no existing file
			prefix := config.prefix()
			err := ensureDir(prefix)
			if err != nil {
				return err
			}
			// create subdirs
			for i := 0; i < 256; i++ {
				name := config.partPrefix(i)
				err := ensureDir(name)
				if err != nil {
					return err
				}
			}

			writeREADME(dir)

			return writeString(final)
		} else {
			// ignore maxAge value - read from config.json
			err = json.Unmarshal([]byte(old), &m)
			if err != nil {
				return err
			}
			maxAge, err = time.ParseDuration(m["maxAge"])
			if err != nil {
				return err
			}
			if maxAge < time.Second*10 {
				return fmt.Errorf("maxAge too short: %s", maxAge)
			}
			config.maxAge = maxAge
		}

		return nil
	}

	g := config.globalLock()
	err := UpdateMultiprocess(g.lockfile, EXCLUSIVE_LOCK, g.datafile, updateContent)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func DefaultConfig() (*Config, error) {
	maxAge := 10 * 24 * time.Hour
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "gorun")
	return NewConfig(dir, maxAge)
}

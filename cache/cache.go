package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// requirements:
//   - graceful failure: if locks are no-op, cache should still work mostly ok
//   - protect against user error: if cache-dir is set to root '/', delete operation should delete
//     zero or very few files
//   - if user creates symlinks in cache dir, delete should only delete symlinks
//   - if two or more P race to the same key and one process has started to create entry
//     but fails before completion, another P will take over the task and complete it
//   - out-of-disk space will not corrupt the cache, only fail it
//		=> need validation of entry data, e.g. guard against truncation

// bonus:
//   - create primitive which guarantees one P will execute task, even though
//  	many run at the same time and some will fail halfway during task
//		= same as a cache like this, but can we make it simpler ?

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

func (config *Config) Dir() string {
	return config.dir
}
func (config *Config) global() Lockpair {
	return Lockpair{path.Join(config.dir, "global.lock"), path.Join(config.dir, "config.json")}
}

func NewConfig(dir string, maxAge time.Duration) (*Config, error) {

	if maxAge < 10*time.Second {
		return nil, fmt.Errorf("maxAge minimum is 10 seconds")
	}

	if !utf8.Valid([]byte(dir)) {
		return nil, fmt.Errorf("config dir is not utf8: %q", dir)
	}
	dir = path.Clean(dir) // only ends in trailing slash if root /
	if !path.IsAbs(dir) || strings.Contains(dir, "\x00") {
		return nil, fmt.Errorf("bad characters in config dir : %q", dir)
	}

	config := &Config{dir, maxAge, regexp.MustCompile(`^[a-z0-9]{2}-t$`), regexp.MustCompile(`^[a-z0-9]{40}$`)}

	mkdirAllRace(dir)

	//lockfile, datafile := config.global()
	//lockfile, datafile := config.global()
	m := make(map[string]string)

	updateContent := func(old string, writeString func(new string) error) error {
		m["maxAge"] = maxAge.String()
		m["#info-maxAge"] = "valid units are h, m and s"

		final, err := jsonString(m)
		if err != nil {
			return err
		}

		//fmt.Println("# updateContent", old, final)
		if old == "" { // = no existing file
			prefix := filepath.Join(dir, "data")
			err := ensureDir(prefix)
			if err != nil {
				return err
			}
			// create subdirs
			for i := 0; i < 256; i++ {
				name := filepath.Join(dir, "data", fmt.Sprintf("%02x-t", i))
				err := ensureDir(name)
				if err != nil {
					return err
				}
			}

			return writeString(final)
		} else {
			// ignore maxAge - use existing config.json
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

	g := config.global()
	err := UpdateMultiprocess(g.lockfile, g.datafile, updateContent)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func DefaultConfig() (*Config, error) {
	maxAge := 10 * 24 * time.Hour
	//log := false
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir = path.Join(dir, "gorun")
	return NewConfig(dir, maxAge)
}

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
	hs2 := fmt.Sprintf("%02x-t", part)
	dir := path.Join(config.dir, "data", hs2)

	e := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() { //&& d.Name() == "main" {
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

func (config *Config) safeRemoveAll2(datafile, objdir string) error {
	err := os.Remove(datafile)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return config.safeRemoveAll(objdir)
	}
	return err
}

func (config *Config) safeRemoveAll(objdir string) error {
	// allow delete of folders named
	//   xx-t/xx40/xx8
	//   xx-t/xx40
	// where x = [0-9a-f]
	d1, d2 := "", ""
	if len(filepath.Base(objdir)) == 8 {
		d2 = filepath.Dir(objdir)
	} else {
		d2 = objdir
	}
	d1 = filepath.Dir(d2)
	if !config.re1.MatchString(filepath.Base(d1)) ||
		!config.re2.MatchString(filepath.Base(d2)) {
		panic(fmt.Errorf("removeAll: bad objdir %s", objdir)) // debug
		return fmt.Errorf("removeAll: bad objdir %s", objdir)
	}
	return os.RemoveAll(objdir)
}

func (config *Config) DeleteExpiredItems() error {
	// part 1 : cache can operate while we delete since we
	//          use fine-grained per-item locks
	var saveError error

	// simplify cache cleaup by hold exclusive lock while we look
	// for expired items
	Lockedfile(config.global().lockfile, ExclusiveLock, func() error {

		for k := 0; k < 256; k++ {
			err2 := config.DeleteExpiredPart(k)
			if err2 != nil {
				saveError = err2
			}
		}
		return nil
	})

	return saveError
}

func lockfile2datafile(lockfile string) string {
	return path.Join(path.Dir(lockfile), "info")
}

func (config *Config) DeleteExpiredPart(part int) error {
	// assume we are running under an exclusive lock on the
	// whole cache
	if part < 0 {
		part = 0
	}
	part = part % 256
	hs2 := fmt.Sprintf("%02x-t", part)
	glob := path.Join(config.dir, "data", hs2, "*", "lock")

	flist, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("glob failed - %w", err)
	}

	var saveError error
	for _, lockfile := range flist {
		err = config.DeleteHash(lockfile)

		if err != nil {
			saveError = fmt.Errorf("error during delete of %s : %s", lockfile, err)
		}
	}
	return saveError
}
func (config *Config) DeleteHash(lockfile string) error {
	datafile := lockfile2datafile(lockfile)

	buf, err := os.ReadFile(datafile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.safeRemoveAll(filepath.Dir(lockfile))
		}
		return err
	}

	obj, err := str2item(string(buf))
	if err != nil {
		// unknown format => avoid deletion
		return err
	}

	// delete items older than maxAge
	age := obj.age()

	if age > config.maxAge {
		// important to first delete datafile
		// (should exist since we just read it)
		err = os.Remove(datafile)
		if err != nil {
			return err
		}

		// delete all files, including lockfile
		return config.safeRemoveAll(filepath.Dir(lockfile))
	}
	return nil
}

//const tsFormat = "2006-01-02T15:04:05.999Z07:00"

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
	//var t time.Time
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
	//t, err = time.Parse(tsFormat, e[2])
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
func (config *Config) prefix() string {
	return path.Join(config.dir, "data")
}

func (config *Config) Lookup2(input string, userCreate func(outDir string) error, useCache bool) (string, error) {
	hs := hashString(input)
	prefix := path.Join(config.prefix(), hs[0:2]+"-t", hs[0:40]) // use 160 bits
	err := ensureDir(prefix)
	if err != nil {
		return "/invalid/outdir/1", fmt.Errorf("failed to create prefix dir %q - %w", prefix, err)
	}
	lockfile := path.Join(prefix, "lock")
	datafile := path.Join(prefix, "info")

	outdir := path.Join(prefix, randomHash()[0:8]) // 8 chars = 32 bits
	updateContent := func(old string, writeString func(new string) error) error {

		if old == "" {
			// object not created yet
			err := os.Mkdir(outdir, 0777)
			if err != nil {
				return fmt.Errorf("outdir %q already exists - program error", outdir)
			}
			err = userCreate(outdir)
			if err != nil {
				config.safeRemoveAll2(datafile, outdir)
				return err
			}
			var obj Item
			obj.objdir = outdir
			obj.refresh()
			err = writeString(item2str(obj))
			if err != nil {
				config.safeRemoveAll2(datafile, outdir)
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
				//fmt.Printf("### age=%s => refresh file %q\n", age, datafile)
				obj.refresh()
			}
			err = writeString(item2str(obj))
			if err != nil {
				config.safeRemoveAll2(datafile, outdir)
				return fmt.Errorf("cache refresh failed for file %q - %w", datafile, err)
			}

		}
		return nil
	}

	withGlobalLock := func() error {
		return UpdateMultiprocess(lockfile, datafile, updateContent)
	}
	err = Lockedfile(config.global().lockfile, SharedLock, withGlobalLock)
	if err != nil {
		return "/invalid/outdir/2", err
	}
	return outdir, nil
}

func mkdirAllRace(dir string) error {
	// safe for many processes to run concurrently
	if !path.IsAbs(dir) {
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
	missing, err = missingFolders(dir, []string{})
	if err != nil {
		return fmt.Errorf("failed to create folder %s - %w", dir, err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("failed to create folder %s", dir)
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
		missing = append(missing, dir)
		d2 := path.Dir(dir)
		if d2 == dir {
			break
		}
		dir = d2
	}
	return []string{}, fmt.Errorf("program error at folder: %s", dir)
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

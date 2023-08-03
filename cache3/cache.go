package cache2

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// examples:
// - if key always = "0" => the cache will only cache one item
// - if key only "1", "2", .. "10" => cache will keep 10 items
// and so on

// requirements:
//   - graceful failure: if locks are no-op, cache should still work mostly ok
//   - protect against user error: if cache-dir is set to root '/', delete operation should delete
//     zero or very few files
//   - if user creates symlinks in cache dir, delete should only delete symlinks
//   - if two or more P race to the same key and one process has started to create entry
//     but fails before completion, another P will take over the task and complete it
//   - out-of-disk space will no corrupt the cache, only fail it
//		=> need validation of entry data, e.g. guard against truncation

// bonus:
//   - create primitive which guarantees one P will execute task, even though
//  	many run at the same time
//		= same as a cache like this, but can we make it simpler ?

type LookupResult int

const (
	FOUND LookupResult = 16
	NEW   LookupResult = 17
	ERROR LookupResult = 99
)

const objectsFolder = "objects"

const infoSuffix = ".text" // clients must not use this suffix

type Config struct {
	dir string // no trailing slashes

	maxAge time.Duration // safe to delete objects older than this
	/*
		refreshAge      time.Duration  // reset age of objects older than this
		lookupUntilRead time.Duration  // after release of lock, for how long is object guarantee to exist
		reHash          *regexp.Regexp // validate hashstring

		testTime    *time.Time
		testLog     string
		testDelayMs map[string]int
	*/
}

/*
func (config *Config) now() time.Time {
	if config.testTime != nil {
		return *config.testTime
	}
	return time.Now()
}
*/

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

func NewConfig(dir string, maxAge time.Duration) (*Config, error) {
	// maxAge  refresh    grace
	// 5 days     12 h     1 h
	// 10 min      1 min   1 min
	if maxAge < 10*time.Second {
		return nil, fmt.Errorf("maxAge minimum is 10 seconds")
	}
	/*
		refreshAge := maxAge / 10
		lookupGraceDuration := maxAge / 10
		if lookupGraceDuration > 1*time.Hour {
			lookupGraceDuration = 1 * time.Hour
		}
	*/
	//id := ""
	if !utf8.Valid([]byte(dir)) {
		return nil, fmt.Errorf("config dir is not utf8: %q", dir)
	}
	dir = path.Clean(dir) // only ends in trailing slash if root /
	if !path.IsAbs(dir) || strings.Contains(dir, "\x00") {
		return nil, fmt.Errorf("bad characters in config dir : %q", dir)
	}

	mkdirAllRace(dir) // review !

	lockfile := path.Join(dir, "global.flock")
	datafile := path.Join(dir, "config")

	updateContent := func(old string, writeString func(new string) error) error {
		final := "done"
		//fmt.Println("# updateContent", old, final)
		if old != final {
			// create subdirs
			for i := 0; i < 256; i++ {
				name := filepath.Join(dir, fmt.Sprintf("%02x", i))
				err := os.Mkdir(name, 0777)
				if err != nil {
					return err
				}
			}

			return writeString(final)
		}

		return nil
	}

	err := UpdateMultiprocess(lockfile, datafile, updateContent)
	if err != nil {
		return nil, err
	}
	return &Config{dir, maxAge}, nil
}

func (config *Config) DeleteExpiredItems(part int) error {
	if part < 0 {
		part = 0
	}
	part = part % 256
	hs2 := fmt.Sprintf("%02x", part)
	glob := path.Join(config.dir, hs2, "*.lock")
	//fmt.Println("# del", glob)
	flist, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("glob failed - %w", err)
	}
	for _, lockfile := range flist {
		fmt.Printf("delete scan %s\n", lockfile)
		datafile := lockfile[0:len(lockfile)-5] + ".info"
		Lockedfile(lockfile, ExclusiveLock, func() error {
			buf, err := os.ReadFile(datafile)
			if err == nil {
				obj, err := str2item(string(buf))
				if err == nil {
					// delete items older than maxAge
					age := obj.age()
					if age > config.maxAge {
						//fmt.Printf("*** expired item found, age=%s\n", age)
						//fmt.Printf("*** dir=%s\n", obj.objdir)

						// TODO: clean up all folders inside given hash
						// in case partial create leftovers there
						os.RemoveAll(obj.objdir)
					}
				} else {
					fmt.Printf("#x - item parse failed\n")
				}

				// is expired ?

			}
			return nil
		})
	}
	return nil
}

const tsFormat = "2006-01-02T15:04:05.999Z07:00"

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
	rand.Seed(time.Now().UnixNano())
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

func (config *Config) Lookup2(input string, create func(outDir string) error, useCache bool) (string, error) {
	hs := hashString(input)
	prefix := path.Join(config.dir, hs[0:2], hs)
	err := ensureDir2(prefix)
	if err != nil {
		return "/invalid/outdir-1", fmt.Errorf("failed to create prefix dir %q - %w", prefix, err)
	}
	lockfile := prefix + ".lock"
	datafile := prefix + ".info"

	outdir := path.Join(prefix, randomHash()[0:8]) // 8 chars = 32 bits
	updateContent := func(old string, writeString func(new string) error) error {

		if old == "" {
			// object not created yet
			err := os.Mkdir(outdir, 0777)
			if err != nil {
				return fmt.Errorf("outdir %q already exists - program error", outdir)
			}
			err = create(outdir)
			if err != nil {
				return err
			}
			var obj Item
			obj.objdir = outdir
			obj.refresh()
			err = writeString(item2str(obj))
			if err != nil {
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
				fmt.Printf("### age=%s => refresh file %q\n", age, datafile)
				obj.refresh()
			}
			err = writeString(item2str(obj))
			if err != nil {
				return fmt.Errorf("cache refresh failed for file %q - %w", datafile, err)
			}

		}
		return nil
	}
	err = UpdateMultiprocess(lockfile, datafile, updateContent)
	if err != nil {
		return "/invalid/outdir", err
	}
	return outdir, nil
}

/*
func (config *Config) log(msg string) {
	if config.testLog != "" {
		fmt.Printf("%s\n", msg)
	}
}

func (config *Config) log2(msg string) {
	if config.testLog != "" {
		fmt.Printf("%d : %s\n", time.Now().UnixMilli()%1000, msg)
	}
}

func (config *Config) testDelay(key string) {
	if config.testDelayMs != nil {
		if config.testDelayMs[key] > 0 {
			n := config.testDelayMs[key]
			config.log(fmt.Sprintf("testDelay %s : %d ms", key, n))
			time.Sleep(time.Millisecond * time.Duration(n))
		} else {
			// fmt.Printf("id %s : warning unknown testDelay %s\n", config.testLog, key)
		}
	}
}

func timeDiff(t1 time.Time, t2 time.Time) time.Duration {
	if t1.After(t2) {
		return t1.Sub(t2)
	} else {
		return t2.Sub(t1)
	}
}
*/

func mkdirAllRace(dir string) error {
	if !path.IsAbs(dir) {
		panic(fmt.Sprintf("program error: dir %s not abs", dir))
	}
	missing := findDir(dir, []string{})

	for i := len(missing) - 1; i >= 0; i-- {
		os.Mkdir(missing[i], 0777) // ignore error as we may race
	}

	// at the end, we want a folder to exist
	// - no matter who created it:
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("failed to create folder %s - %w", dir, err)
	}
	if info.IsDir() {
		return nil
	}
	return fmt.Errorf("failed to create folder %s - unexpected filetype", dir)
}

func findDir(dir string, missing []string) []string {
	info, err := os.Stat(dir)
	if err == nil && info.IsDir() {
		return missing
	}
	d2 := path.Dir(dir)
	missing = append(missing, dir)
	if d2 != dir {
		return findDir(d2, missing)
	}
	return missing
}

func ensureDir2(dir string) error {
	fileinfo, err := os.Stat(dir)
	if err == nil && fileinfo.IsDir() {
		return nil
	}
	if err != nil {
		return os.Mkdir(dir, 0777)
	}
	return nil
}

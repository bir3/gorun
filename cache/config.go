package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
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

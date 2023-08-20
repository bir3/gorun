package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

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
	Lockedfile(config.globalLock().lockfile, ExclusiveLock, func() error {

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

func (config *Config) DeleteExpiredPart(part int) error {
	// assume we are running under an exclusive lock on the
	// whole cache
	glob := filepath.Join(config.partPrefix(part), "*", "lockfile")

	flist, err := filepath.Glob(glob)

	//fmt.Printf("## glob %s = %d items\n", glob, len(flist))

	if err != nil {
		return fmt.Errorf("glob failed - %w", err)
	}

	var saveError error
	for _, lockfile := range flist {
		//fmt.Printf("## deleteHash %s\n", lockfile)
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

	if obj.age() > config.maxAge {
		// important to first delete datafile
		// - must exist since we just read it
		err = os.Remove(datafile)
		if err != nil {
			return err
		}

		// delete all files, including lockfile
		return config.safeRemoveAll(filepath.Dir(lockfile))
	}
	return nil
}

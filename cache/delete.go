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

/*
	if lastTrim + maxAge/10 > time.now()
		mkdir trim-running
		= race, the one who wins, executes trim operation

	if trimdir age is > maxAge/20, feel free to delete it
		= no harm to delete, will only increase delay
*/

func (config *Config) DeleteExpiredItems() error {
	// part 1 : cache can operate while we delete since we
	//          use fine-grained per-item locks
	var saveError error

	for k := 0; k < 256; k++ {
		err2 := config.DeleteExpiredPart(k)
		if err2 != nil {
			saveError = err2
		}
	}

	return saveError
}

func (config *Config) DeleteExpiredPart(part int) error {
	// we run under an exclusive lock on our part of the cache

	withPartLock := func() error {
		// we must only search for lockfiles under an exclusive lock
		// as otherwise an item being created may only have reached
		// the point of creating the lockfile and not yet locked it
		glob := filepath.Join(config.partPrefix(part), "*", "lockfile")

		flist, err := filepath.Glob(glob)

		if err != nil {
			return fmt.Errorf("glob failed - %w", err)
		}

		// NOTE: deleting lockfile is never safe, except under a higher lock,
		// partLock because first we need to create the lockfile and then lock
		// and file could be deleted before we lock (partLock here prevents that)
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
	hash := fmt.Sprintf("%02x", part)
	return Lockedfile(config.partLock(hash).lockfile, ExclusiveLock, withPartLock)
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

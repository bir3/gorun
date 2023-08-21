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
		return fmt.Errorf("removeAll: bad objdir %s", objdir)
	}
	return os.RemoveAll(objdir)
}

func (config *Config) trimPending() bool {
	// return true if we should trim/delete old objects
	// - if any error, we return true

	buf, err := os.ReadFile(config.trimLock().datafile) // unix timestamp of last trim
	if err != nil {
		return true
	} else {
		item, err := str2item(string(buf))
		if err != nil {
			return true
		}
		return item.age() > config.maxAge/10
	}
}

func (config *Config) DeleteExpiredItems() error {

	if !config.trimPending() {
		return nil // fast common path (no lock)
	}

	runTrim := false

	checkIfRefreshNeeded := true
	runTrim, err := config.updateTrimRefreshTime(checkIfRefreshNeeded)
	if err != nil {
		return err
	}
	if runTrim {
		return config.TrimNow()
	}
	return nil
}

func (config *Config) updateTrimRefreshTime(checkIfRefreshNeeded bool) (bool, error) {
	pair := config.trimLock()
	updated := false
	withLock := func() error {
		if checkIfRefreshNeeded && !config.trimPending() {
			return nil // other process P already updated
		}

		item := Item{}
		item.objdir = "/gorun/trim"
		item.refresh()
		err := os.WriteFile(pair.datafile, []byte(item2str(item)), 0666)
		if err != nil {
			return err
		}
		updated = true
		return nil
	}

	err := Lockedfile(pair.lockfile, ExclusiveLock, withLock)
	return updated, err
}

func (config *Config) TrimNow() error {
	var saveError error

	for k := 0; k < 256; k++ {
		err := config.DeleteExpiredPart(k)
		if err != nil && saveError == nil {
			saveError = err
		}
		checkIfRefreshNeeded := false
		_, err = config.updateTrimRefreshTime(checkIfRefreshNeeded)
		if err != nil && saveError == nil {
			saveError = err
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

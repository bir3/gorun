
# File cache for multiple processes

- based on file locks
- will release lock after object creation

warning: result object is protected by time only
- assumes created object will be used before max-age 

config:
    max age of item, typically 10 days
    => refresh after maxAge/10
    => delete scan every maxAge/10 or upon request

objects that are older than max age will be deleted

# file layout

```
$cacheDir/gorun/config.json
$cacheDir/gorun/config.lock
$cacheDir/gorun/trim.txt
$cacheDir/gorun/trim.lock
$cacheDir/gorun/README

$cacheDir/gorun/xx-t/lockfile
$cacheDir/gorun/xx-t/xxyyy/lockfile
$cacheDir/gorun/xx-t/xxyyy/info
$cacheDir/gorun/xx-t/xxyyy/     = folder owned by lockfile
$cacheDir/gorun/xx-t/xxyyy/zzzz = object creation folder, always new and uniq

xx/yy/zz regexp [0-9a-f]
```

# requirements

- if two or more P race to the same key and one process has started to create entry
  but fails before completion, another P will take over the task
- protect against user error: if cache-dir is set to root '/', delete operation should delete
  zero or very few files
- if user creates symlinks in cache dir, delete should only delete symlinks
- out-of-disk space should not corrupt the cache, only fail it
  => need validation of entry data, e.g. guard against truncation
- graceful failure: if locks are no-op, cache should still work mostly ok




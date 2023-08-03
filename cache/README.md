
# File cache for multiple processes

- based on file locks
- will release lock after object creation

warning: result object is protected by time only
- assumes created object will be used before max-age 

config:
    max age of item, typically 10 days
    => refresh after max/2
    => delete scan every max/10 or upon request

objects that are older than max age will be deleted

# file layout

```
$cacheDir/gorun/config.json
$cacheDir/gorun/README

$cacheDir/gorun/xx/xxyyy.flock
$cacheDir/gorun/xx/xxyyy/ = folder owned fy lockfile
$cacheDir/gorun/xx/xxyyy/zzzz = object creation folder, always new and uniq

xx/yy/zz regexp [0-9a-f]
```

# api:

```go
// convenient api
// input should be a string that represents a complete description of a
// repeatable computation (command line equivalent, environment variables,
// input file contents, executable contents).
// returns outdir
func Lookup(input string, create func(outDir string) error) (string, error)
// 
// advanced users for more control
func DefaultConfig() (*Config, error)

// minimum maxAge is 10 seconds
// if config exists already, maxAge is ignored
// verify: assuming duration is seconds will fail up to 40 days
func NewConfig(cacheDir string, maxAge time.Duration) (*Config, error)

// if useCache is false => create is always called with new outDir value
func (config *Config) Lookup(input string, create func(outDir string) error, useCache bool) (string, error)
func (config *Config) MaxAge() time.Duration
// 

// cache is divided into 256 parts, loop over 0-255 to delete
// all expired items
func (config *Config) DeleteExpiredItems(part int) error

type Info struct {
    itemCount int
    totalItemSizeBytes int64
}
func (config*Config) GetInfo() (Info, error)

func (config *Config) DeleteExpiredItems(part int) error

// delete all cache items 
// - only safe if no concurrent cache usage
func (config *Config) UnsafeDeleteAll() error

#delete:
write $cacheDir/space2  # if we refresh old flock file, write as -2.flock

read .flock file
    age = old ? delete file

#update old
read .flock file
    age = old ?
    refresh age
    close file
read .flock file


```

# delete protocol

```
will always lock object before deletion, to avoid race condition
and abort deletion if object is no longer expired after receiving lock

this will leave the .flock items still present;

to delete .flock items:
always get read lock for whole $cacheDir
and to delete, we will get write lock, meaning exclusive access to $cacheDir
and thus safe to delete expired .flock files
```    

# api misuse

```
- multiple goroutines lookup the same item ?
- multiple goroutines run Config ?
```

# option: no-cache == verify

means two things:
= delete outdir after use
= always create new outdir, suitable to verify cache output

# robustness: detect slow executions

gorun can startup check if .flock file is now expired or older than max/5 
=> that would mean our assumption about fast start is failing
(could mean heavily overloaded system)
=> append-log event that this occured

# special properties of file caches

cached objects are stored on the shared filesystem, this means that we must
promise not to delete the object until being fairly sure the client/user has retrieved 
the object or no longer needs it

# robustness check: long paths

there could be filesystems that silently truncated long filenames, e.g. treat 
a shorter version of the path as equivalent to the long path
=> we can do a basic test for this at runtime

# robustness check: time

the filesystem could be networked and thus be using a different clock
=> we can test this by creating a file and checking difference vs. local clock


# robustness: input key is always the same '0'

the client may not correctly collect all the inputs that affect the output computation
=> we will give a cached result that is wrong, e.g. running the computation would have yielded 
a different result

# idea: splitting storage by time interval to simplify deletion

if max-age of objects is set to say 10 days, we can partition the file storage
into interval by diving current time by 10 days with modulus 5


downside: after 10 days, the object will always need to be re-created even if frequently used
downside: if we have no activity for say 30 days, we will need to rebuild the cache

# cache ideas that do not work

## limiting keyspace

say we want to limit the number of keys we work with, this could be a way to limit the 
maximum potential cache size.  The key could be like '0' .. '10'.

now the result object must be valid for some time, so we will not be able 
to reduce cache size with this method

also, if we hold a lock on the input key, we will block clients with the same key
until the key is released




# delete protcol

```
first, we have a refresh protocol so if we access object
that is older than max-age/10 we refresh age of object (typically 1 day)

before delete we always lock file
$cacheDir/gorun/xx/xxyyy.flock
and only delete object if older than max-age
=> this way we avoid deleting recently accessed objects

example race:
    max-age is 10 days
    a process P1 lookups object aged 9.99999 days
    and releases lock, prepare to exec into object (or use object)

    a process P2 lookups object and finds its age 10.000001 days
    allowing deletion

    to avoid this scenario, we refresh object if its age is older than
    max-age/10

    however this tells of an edge case in case of a sleeping/hibernating computer
    a process P1 finds object aged 0.99999 days, releases lock
    and prepares to exec into it, but sleeps just before

    computer wakes up, but delete process P2 now locks object
    and finds its age to be 15 days and promptly deletes it
    => process P1 fails as the cached object has suddenly vanished

    another race condition: if we store all objects at a single key '0'
    and a process P1 locks '0', create object, releases lock, prepares to exec
    then process P2 locks '0', but creates a new object, releases lock, and context
    switches back to P1 which exec's, but runs the wrong code from P2

```

# api

```go
// example key:
// - absolute filepath
// - string from "1" .. "10" => cache will hold max 10 items
//
// content must be representation of contents, can be hash but need not be
//
// return value
//    (folder, error)
//
// (internally, Lookup will hash both key and object to get internal uniformity)
// ? drop config and use default for simplicity ?
func (config *Config) Lookup(key string, content string, user_create func(folder string) error) (string, error) {
	return config.Lookup2(key, hashOfInput, user_create, func() error { return nil })
}

```
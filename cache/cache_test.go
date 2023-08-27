// Copyright 2023 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

/*
	cache_functional_test.go
		= basic functional test of the cache, mostly black-box
		= also test with multiple running processes

	cache_race_test.go
		= test race conditions, white box
		= potentially complicated and relying on implementation

# requirements:

  - repeated lookups always returns same cached entry
    and stored count is 1

  - lookup with interval longer than expire interval will return new object
    and stored count remains at 1 as expired object is automatically deleted

  - multiple lookups of same key return same cached entry and only one
    performs job
    and if any process fails midway, some other process will complete the job
    with a new object id, e.g. it will not reuse prior partial dir

# core abstraction

OnceMultiprocess.Do(sharedfilename string, f func())
  - will return when f has completed by some process

# TODO:

  - TestDelete: review and make solid, must test a specific property that we verify
    and test both cases: delete and not delete
    should also test refresh lock file

- add normal cache use, verify performance, e.g. just basic sanity
- cache2: cleanup multiple objects per skey as there is only one owner

- cache2: later: add special cleanup mode that can size-limit cache and cleanup empty folders and stale .lock files

explore: CCT testing

	https://wcventure.github.io/pdf/ICSE2022_PERIOD.pdf
*/

func tmpdir(t *testing.T) string {

	d := os.Getenv("GORUN_TESTDIR")
	if d != "" {
		os.Mkdir(d, 0777) // assume many will race, so ignore error
		d = filepath.Join(d, t.Name())
		os.Mkdir(d, 0777)
		return d
	}
	return t.TempDir()
}

func TestMain(m *testing.M) {

	get0 := func(key string) (value string, found bool) {
		for _, item := range os.Args[1:] {
			k, v, ok := strings.Cut(item, "=")
			if !ok {
				panic(fmt.Sprintf("item missing '=' : %s", item))
			}
			if key == k {
				found = true
				value = v // last key wins
				prefix := "data:text/plain;base64,"
				if strings.HasPrefix(v, prefix) {
					buf, err := base64.StdEncoding.DecodeString(v[len(prefix):])
					if err != nil {
						panic(err)
					}
					value = string(buf)
				}
			}
		}
		return value, found
	}

	get := func(key string) string {
		value, found := get0(key)
		if !found {
			panic(fmt.Sprintf("key %s not found in commandline: %s", key, strings.Join(os.Args, " ")))
		}
		return value
	}

	getDuration := func(key string) time.Duration {
		d, err := time.ParseDuration(get(key)) // examples: 5s  100ms
		if err != nil {
			panic(err)
		}
		if d != 0 && d < time.Millisecond {
			panic("minimum duration is 1ms except for zero duration")
		}
		return d
	}

	found := len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "func=")

	if !found {
		os.Exit(m.Run()) // Go tests
	}
	switch get("func") {
	case "lookup":
		lookup(get("line"), get("key"), getDuration("startDelay"), getDuration("createDelay"), get("tmp"))
	default:
		log.Fatalf("unknown func %s", get("func"))
	}
}

func str2base64(s string) string {
	return "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(s))
}

func validateLine(line string) {
	_, err := strconv.Atoi(line)
	if err != nil {
		panic(err)
	}
}

func template2str(t *testing.T, templateString string, m map[string]string) string {
	exe, err := os.Executable()
	if err != nil {
		panic(fmt.Sprintf("can't find file name of executable: %s", err))
	}
	if m == nil {
		m = make(map[string]string)
	}
	d := t.TempDir()
	m["exe"] = exe
	m["tmp"] = str2base64(d)

	var buffer = new(bytes.Buffer)
	template.Must(template.New("").Parse(templateString)).Execute(buffer, m)

	return buffer.String()
}

func TestCache(t *testing.T) {
	// verify items expire
	t.Parallel()
	cacheDir := tmpdir(t)
	// debug: rm -rf tmp; GORUN_TESTDIR=$(pwd)/tmp go test ./cache

	config, err := newConfig(cacheDir, time.Millisecond*30)

	create := func(key string) {
		_, err := config.Lookup(key, func(objdir string) error {
			err := os.WriteFile(objdir+"/some-file", []byte("abc"), 0666)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if err != nil {
		t.Fatalf("failed to create cache %s", err)
	}

	create("bb")
	create("b2")
	expectCountFiles(t, cacheDir, "some-", 2)

	// advance time so that cache is expired
	time.Sleep(time.Millisecond * 40)

	config.TrimPeriodically()

	create("b3")
	expectCountFiles(t, cacheDir, "some-", 1)

}

func TestRefresh(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	config, err := newConfig(cacheDir, time.Millisecond*30)

	if err != nil {
		t.Fatalf("failed to create cache %s", err)
	}

	var isNew bool
	objdir1, err := config.Lookup("bb", func(objdir string) error {
		isNew = true
		err := os.WriteFile(objdir+"/some-file", []byte("abc"), 0666)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Fatalf("missing create")
	}

	time.Sleep(time.Millisecond * 10)

	// verify repeated lookups keep item alive past normal expire

	for i := range []int{1, 2, 3, 4, 5, 6} {
		objdir2, err := config.Lookup("bb", func(objdir string) error {
			t.Fatalf("unexpected create event, at i=%d", i)
			// flaky: cache_test.go:229: unexpected create event
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond * 10)
		config.TrimNow()
		if objdir1 != objdir2 {
			t.Fatalf("failed")
		}
	}

}

func TestHash(t *testing.T) {
	// find simple keys that have the same hash (for tests)
	// GORUN_HASH=a go test -v ./cache -run TestHash
	if os.Getenv("GORUN_HASH") == "" {
		t.Skip()
	}
	input := os.Getenv("GORUN_HASH")
	inputHash := hashString(input)[0:2]
	fmt.Printf("%s has hash %s\n", input, inputHash)
	// find short string that has same hash prefix
	// as another string
	limit := 10
	for i := 0; i < 256*100; i++ {
		n := i
		s := ""
		for n > 0 {
			ch := 'a' + (n % 26)
			n = n / 26
			s = fmt.Sprintf("%s%c", s, ch)
		}
		hs := hashString(s)
		if hs[0:2] == inputHash {
			fmt.Printf("%s has same hash %s @ %d\n", s, hs[0:2], i)
			limit -= 1
			if limit == 0 {
				break
			}
		}

	}
}
func TestLookup(t *testing.T) {
	// test basic cache lookup
	t.Parallel()

	out := runProcessList(t, template2str(t, `
		{{$.exe}} func=lookup key=a startDelay=0ms  createDelay=0ms tmp={{$.tmp}}
		{{$.exe}} func=lookup key=a startDelay=50ms createDelay=0ms tmp={{$.tmp}}
		{{$.exe}} func=lookup key=a startDelay=50ms createDelay=0ms tmp={{$.tmp}}
		`, nil))

	s := strings.Join(out, ",")
	expect := "NEW,FOUND,FOUND"
	if s != expect {
		t.Fatalf("got %s but expected %s", s, expect)
	}
}

func TestLookup2(t *testing.T) {
	// verify concurrent create run in parallel
	t.Parallel()
	// GORUN_HASH=aa go test -v ./cache -run TestHash
	// => shows key 'pm' has same hash prefix

	t1 := time.Now()
	out := runProcessList(t, template2str(t, `
		{{$.exe}} func=lookup key=aa startDelay=0ms   createDelay=150ms tmp={{$.tmp}}
		{{$.exe}} func=lookup key=pm startDelay=50ms  createDelay=100ms tmp={{$.tmp}}
		{{$.exe}} func=lookup key=aa startDelay=100ms  createDelay=0ms  tmp={{$.tmp}}
		`, nil))
	dt := time.Since(t1).Abs()

	// verify that two concurrent create objects do not block each other
	if dt > time.Millisecond*200 {
		// if concurrent, we expect 150ms
		// else 250ms
		t.Fatalf("cache too slow: %s", dt)
		// flaky: cache_test.go:332: cache too slow: 278.636916ms
		// flaky: cache_test.go:331: cache too slow: 324.422417ms
		// go test -v ./cache -run TestLookup2 -count 1
	}
	fmt.Printf("dt = %s\n", dt)
	s := strings.Join(out, ",")
	expect := "NEW,NEW,FOUND"
	if s != expect {
		t.Fatalf("got %s but expected %s", s, expect)
	}
}

func lookup(line, key string, startDelay, createDelay time.Duration, cacheDir string) {
	// runs in a separate subprocess

	validateLine(line) // line starts at "0", "1", ...
	config, err := NewConfig(cacheDir, 10*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(8)
	}

	time.Sleep(startDelay)

	found := true
	create := func(outdir string) error {
		found = false
		time.Sleep(createDelay)
		return nil
	}
	objDir, err := config.Lookup(key, create)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(8)
	}
	if !filepath.IsAbs(objDir) {
		panic(fmt.Sprintf("Lookup did not return abs path, got objDir=%s", objDir))
	}
	if found {
		fmt.Printf("FOUND")
	} else {
		fmt.Printf("NEW")
	}
}

func expectCountFiles(t *testing.T, dir string, prefix string, n int) {
	nactual := countFiles(dir, "some-")
	if n != nactual {
		t.Fatalf("expected %d files but found %d", n, nactual)
	}
}

func countFiles(d string, prefix string) int {
	fileSystem := os.DirFS(d)
	n := 0
	err := fs.WalkDir(fileSystem, ".", func(fspath string, entry fs.DirEntry, err error) error {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), prefix) {
			n++
		}
		return err
	})
	if err != nil {
		panic(err)
	}
	return n
}

func Create2(d string, maxAge time.Duration, _ bool) (*Config, error) {
	return NewConfig(d, maxAge)
}

func TestInternals(t *testing.T) {
	// internal sanity checks

	// verify refresh
	var obj Item
	obj.refresh()
	if obj.age() > time.Second*10 {
		t.Fatal("fresh object should not be old")
	}
	if obj.age() < 0 {
		t.Fatal("negative age")
	}
	config, err := newConfig(t.TempDir(), time.Millisecond*200)
	if err != nil {
		t.Fatal(err)
	}

	// minimal sanity of hash
	for i := 0; i < 1000; i++ {
		hash := hashString(fmt.Sprintf("%d", i))
		d1 := config.partPrefixFromHash(hash)
		i, err := strconv.ParseInt(hash[0:2], 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		d2 := config.partPrefix(int(i))
		if d1 != d2 {
			t.Fatal(err)
		}
	}

}

func TestDelete(t *testing.T) {
	t.Parallel()
	d := t.TempDir()

	config, err := newConfig(d, time.Millisecond*200)
	if err != nil {
		t.Fatal(err)
	}
	createObj(config, "aa")
	createObj(config, "bb")
	config.TrimPeriodically()
	expectCountFiles(t, d, "some-", 2)

	time.Sleep(210 * time.Millisecond) // all objects expired by now
	fmt.Println("---- after sleep 210ms ----")

	createObj(config, "bb")

	config.TrimPeriodically()

	log.Printf("*** after wait\n")

	expectCountFiles(t, d, "some-", 1)

}

func createObj(config *Config, hashOfInput string) {
	_, _ = config.Lookup(hashOfInput, func(objdir string) error {
		err := os.WriteFile(objdir+"/some-"+hashOfInput+"-file", []byte(hashOfInput+hashOfInput), 0666)
		if err != nil {
			panic(err)
		}
		log.Printf("create file\n")
		return nil
	})
}

type CmdResult struct {
	id  int
	out string
	err error
}

func filter(slist []string, f func(s string) bool) []string {
	out := []string{}
	for _, s := range slist {
		if f(s) {
			out = append(out, s)
		}
	}
	return out
}

func validLine(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !strings.HasPrefix(s, "#")
}

func shellsplit(s string) []string {
	// split string on whitespace but quote aware
	// - useful for subprocess invocation
	// note: we assume quotes are balanced
	s = strings.TrimSpace(s)
	isQuote := func(x byte) bool {
		return x == '\'' || x == '"'
	}
	isSpace := func(x byte) bool {
		return x == '\t' || x == ' '
	}
	var q byte
	q = '.' // = not set
	// simple quote aware split

	var begin int = -1 // not field found
	out := []string{}
	addfield := func(s string) {
		if len(s) > 1 && isQuote(s[0]) && isQuote(s[len(s)-1]) {
			s = s[1 : len(s)-1]
		}
		out = append(out, s)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if q == '.' {
			if isQuote(c) {
				q = c
			}
		} else if q == c {
			q = '.'
		}
		space := q == '.' && isSpace(c)

		if begin < 0 {
			if !space {
				begin = i
			}
		} else {
			if space {
				// field found
				addfield(s[begin:i])
				begin = -1
			}
		}
	}
	if begin >= 0 {
		addfield(s[begin:])
	}
	return out
}

func TestShellsplit(t *testing.T) {
	type Example struct {
		input  string
		expect []string
	}
	examples := []Example{
		{"", []string{}},
		{" ", []string{}},
		{"   ", []string{}},
		{" a ", []string{"a"}},
		{"  a  ", []string{"a"}},
		{"a b", []string{"a", "b"}},
		{"a 'b ' c", []string{"a", "b ", "c"}},
		{`a "b " c`, []string{"a", "b ", "c"}},
	}
	for _, x := range examples {
		actual := shellsplit(x.input)
		if slices.Compare(actual, x.expect) != 0 {
			t.Fatalf("input %s - expected %s but got %s", x.input, x.expect, actual)
		}
	}
}

/*
run a list of processes in parallel and return the output (stdout+stderr)
as list of strings, in the same order as submitted, example:

pspec="
python3 -c 'print(4)'
python3 -c 'print(5)'
"

returns ["4\n", "5\n"]
*/

func TestProcesslist(t *testing.T) {
	out := runProcessList(t, `
	python3 -c 'import time; time.sleep(0.1); print(4)'
	python3 -c 'print(5)'
		`)
	expect := []string{"4\n", "5\n"}
	if slices.Compare(out, expect) != 0 {
		t.Fatalf("unexpected output %s", out)
	}
}

func runProcessList(t *testing.T, pspec string) []string {
	resList := runProcessList2(t, pspec)
	var out []string
	for _, res := range resList {
		if res.err != nil {
			t.Fatalf("runPspec failed: %v %d\n##\n%s ##", res.err, len(res.out), res.out)
		}
		out = append(out, res.out)
	}
	return out
}
func runProcessList2(t *testing.T, pspec string) []CmdResult {
	plines := filter(strings.Split(pspec, "\n"), validLine)

	nproc := len(plines)
	rx := make(chan CmdResult, nproc)
	for k := 0; k < nproc; k++ {
		k := k
		go func() {
			args := shellsplit(plines[k])
			if len(args) > 1 && strings.HasPrefix(args[1], "func=") {
				args = append(args, fmt.Sprintf("line=%d", k))
			}
			cmd := exec.Command(args[0], args[1:]...) //exe, args...)
			out, err := cmd.CombinedOutput()
			rx <- CmdResult{k, string(out), err}
		}()
	}

	var out []CmdResult = make([]CmdResult, nproc)
	for k := 0; k < nproc; k++ {
		select {
		case res := <-rx:
			out[res.id] = res
		case <-time.After(1 * time.Second):
			t.Fatalf("timeout after 1 sec")
		}
	}

	return out
}

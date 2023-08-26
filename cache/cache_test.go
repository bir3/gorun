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

func template2str(templateString string, m map[string]any) string {
	exe, err := os.Executable()
	if err != nil {
		panic(fmt.Sprintf("can't find file name of executable: %s", err))
	}
	if m == nil {
		m = make(map[string]any)
	}
	m["exe"] = exe

	var buffer = new(bytes.Buffer)
	template.Must(template.New("").Parse(templateString)).Execute(buffer, m)

	return buffer.String()
}

/*
 */
func TestCache(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	//testTime := time.Now() // we control the clock now
	config, err := Create2(cacheDir, time.Second*30, false)
	//config.testTime = &testTime //timeNow = func() time.Time { return testTime }

	if err != nil {
		t.Fatalf("failed to create cache %s", err)
	}
	olist := make([]string, 0, 100) // high capacity so we can pass by reference

	//fmt.Println("* time t0 ", testTime)
	objdir1 := newEntry(t, config, "bb", olist)
	objdir2 := newEntry(t, config, "b2", olist)

	if objdir1 == objdir2 {
		t.Fatalf("failed")
	}
	// advance time so that cache is expired
	// testTime = testTime.Add(time.Millisecond * 31)
	time.Sleep(time.Millisecond * 40)
	fmt.Println("========================")
	objdir3 := newEntry(t, config, "b3", olist)

	if objdir3 == objdir1 || objdir3 == objdir2 {
		t.Fatalf("lookup returned old existing object %s !", objdir3)
	}

}

func TestRefresh(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	config, err := newConfig(cacheDir, time.Millisecond*30)

	if err != nil {
		t.Fatalf("failed to create cache %s", err)
	}
	olist := make([]string, 0, 100) // high capacity so we can pass by reference

	objdir1 := newEntry(t, config, "bb", olist)
	time.Sleep(time.Millisecond * 10)

	// verify repeated lookups keep item alive past normal expire

	for range []int{1, 2, 3, 4, 5, 6} {
		objdir2, err := config.Lookup("bb", func(objdir string) error {
			t.Fatalf("unexpected create event")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond * 10)
		if objdir1 != objdir2 {
			t.Fatalf("failed")
		}
	}

}

func newEntry(t *testing.T, config *Config, hashOfInput string, olist []string) string {
	n := len(olist)
	isNew := false
	objdir1, err := config.Lookup(hashOfInput, func(objdir string) error {
		isNew = true
		os.WriteFile(objdir+"/some-"+hashOfInput+"-file", []byte(hashOfInput+hashOfInput), 0666)
		olist = append(olist, objdir)
		//fmt.Println("* x1")
		return nil
	})
	if err != nil {
		t.Fatalf("Lookup failed with %s", err)
	}
	//fmt.Println("* x2")
	if len(olist) != n+1 {
		t.Fatalf("failed to get create event for input %s", hashOfInput)
	}
	if !isNew || !filepath.IsAbs(objdir1) {
		t.Fatalf("failed for hashOfInput %s", hashOfInput)
	}

	found := true
	objdir2, err := config.Lookup(hashOfInput, func(objdir string) error {
		t.Fatalf("did not expect create event hashOfInput %s", hashOfInput)
		found = false
		return nil
	})
	if err != nil {
		t.Fatalf("Lookup failed with %s", err)
	}
	if !found || objdir1 != objdir2 || !filepath.IsAbs(objdir2) {
		t.Fatalf("lookup after create failed for hashOfInput %s", hashOfInput)
	}
	return objdir2
}

func TestLookup(t *testing.T) {
	t.Parallel()

	d := t.TempDir()
	m := map[string]any{"tmp": str2base64(d)}

	out := runProcessList(t, template2str(`
		{{$.exe}} func=lookup key=a startDelay=0ms  createDelay=0ms tmp={{$.tmp}}
		{{$.exe}} func=lookup key=a startDelay=50ms createDelay=0ms tmp={{$.tmp}}
		{{$.exe}} func=lookup key=a startDelay=50ms createDelay=0ms tmp={{$.tmp}}
		`, m))
	s := strings.Join(out, ",")
	expect := "NEW,FOUND,FOUND"
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
	objDir, err := config.Lookup("a7a7", create)
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

	//m := map[string]any{"tmpdir64": str2base64(d)}

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

package cache2

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/exec"
	"path"
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

func str2base64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
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

	config, err := Create2(cacheDir, time.Second*30, false)

	if err != nil {
		t.Fatalf("failed to create cache %s", err)
	}
	olist := make([]string, 0, 100) // high capacity so we can pass by reference

	objdir1 := newEntry(t, config, "bb", olist)
	time.Sleep(time.Millisecond * 10)

	// verify repeated lookups keep item alive past normal expire

	for _ = range []int{1, 2, 3, 4, 5, 6} {
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
	if !isNew || !path.IsAbs(objdir1) {
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
	if !found || objdir1 != objdir2 || !path.IsAbs(objdir2) {
		t.Fatalf("lookup after create failed for hashOfInput %s", hashOfInput)
	}
	return objdir2
}

func TestLookup(t *testing.T) {
	t.Parallel()

	d := t.TempDir()
	m := map[string]any{"tmpdir64": str2base64(d)}

	testLookup(t, template2str(`
		{{$.exe}} --entry=lookupSubprocess --map=tmpdir64={{$.tmpdir64}}
		{{$.exe}} --entry=lookupSubprocess --map=tmpdir64={{$.tmpdir64}}
		{{$.exe}} --entry=lookupSubprocess --map=tmpdir64={{$.tmpdir64}}
		`, m))
	return
	/*
		cleanTmpDir(t)
		out := testLookup(t, template2str(`
			{{$.exe}} --entry=lookupSubprocess --id=0 --delay=lock=20
			{{$.exe}} --entry=lookupSubprocess --id=1 --delay=create-lockfile=10
			{{$.exe}} --entry=lookupSubprocess --id=2 --delay=create-lockfile=10
			`, nil))
		verify(out[0], "lockfile created,FOUND")

		cleanTmpDir(t)
		out = testLookup(t, template2str(`
			{{$.exe}} --entry=lookupSubprocess --id=0
			{{$.exe}} --entry=lookupSubprocess --id=1 --delay=create-lockfile=10
			{{$.exe}} --entry=lookupSubprocess --id=2 --delay=create-lockfile=10
			`, nil))
		verify(out[0], "lockfile created,NEW")
	*/
}

/*
	create objects
	wait expire time
	count objects
	create new object
	verify expired objects are deleted
*/

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

func expectCountFiles(t *testing.T, dir string, prefix string, n int) {
	nactual := countFiles(dir, "some-")
	if n != nactual {
		t.Fatalf("expected %d files but found %d", n, nactual)
	}
}

func Create2(d string, maxAge time.Duration, _ bool) (*Config, error) {
	return NewConfig(d, maxAge)
}

func TestBasic(t *testing.T) {
	var obj Item
	obj.refresh()
	if obj.age() > time.Second*10 {
		t.Fatal("fresh object should not be old")
	}
}

func TestDelete(t *testing.T) {

	t.Parallel()

	d := t.TempDir()
	//m := map[string]any{"tmpdir64": str2base64(d)}

	config, err := Create2(d, time.Second*10, false)
	config.maxAge = time.Millisecond * 200 //
	if err != nil {
		t.Fatal(err)
	}
	createObj(config, "aa")
	createObj(config, "bb")
	expectCountFiles(t, d, "some-", 2)

	for i := 0; i < 256; i++ {
		config.DeleteExpiredItems(i)
	}

	time.Sleep(210 * time.Millisecond) // all objects expired by now
	fmt.Println("---- after sleep 210ms ----")

	createObj(config, "bb")

	for i := 0; i < 256; i++ {
		config.DeleteExpiredItems(i)
	}
	log.Printf("*** after wait\n")

	expectCountFiles(t, d, "some-", 1)

	/*
		out := testLookup(t, template2str(`
			{{$.exe}} --entry=lookupSubprocess --map=tmpdir64={{$.tmpdir64}}
		`, m))
		verify(out[0], "NEW")

		// state: one object now exists in cache

		out = runProcessList(t, template2str(`
			# we run 10ms later than the processes that deletes objects
			# => we will re-create object
			# => expect to see "NEW" = new cache item created
			{{$.exe}} --entry=lookupSubprocess --id=0 --expire_ms=5 --delay=lookup-start=20

			# we set object expire age to 5ms
			# then we wait 10ms so that we have expired objects
			# => existing object will be deleted
			{{$.exe}} --entry=deleteSubprocess --id=1 --expire_ms=5 --delay=delete=10
		`, nil))

		//printList(out)
		if !hasLine(out[0], "NEW") {
			t.Fatalf("TestDelete failed")
		}
	*/
}

func create(objDir string) error {
	if !path.IsAbs(objDir) {
		panic(fmt.Sprintf("program error: create objDir is not abspath : %s", objDir))
	}
	fmt.Println("create called ... = we create cached object in objDir", objDir)
	//time.Sleep(time.Second * 2)
	err := os.WriteFile(path.Join(objDir, "data.txt"), []byte("abc"), 0666)
	if err != nil {
		return err
	}
	return nil
}

func (config *Config) setDelay(s string) {
	// key=20,key2=30,...
	if s != "" {
		for _, x := range strings.Split(s, ",") {
			e := strings.Split(x, "=")
			key, value := e[0], e[1]

			if strings.TrimSpace(key) != key || len(strings.TrimSpace(key)) == 0 {
				panic(fmt.Sprintf("bad key %q", key))
			}
			if strings.TrimSpace(value) != value {
				panic(fmt.Sprintf("bad value %q", value))
			}
			/*
				valueInt, err := strconv.Atoi(value)
				if err != nil {
					panic(fmt.Sprintf("bad initDelay string: %s ; can't convert %s to int", s, value))
				}
				config.testDelayMs[key] = valueInt
			*/
		}
	}
}

type CmdResult struct {
	id  int
	out string
	err error
}

func verify(out string, spec string) {
	expect := strings.Split(spec, ",")
	for _, line := range strings.Split(out, "\n") {
		if len(expect) == 0 {
			break
		}
		if line == expect[0] {
			//fmt.Println("expect ok", expect[0])
			expect = expect[1:]
		}
	}
	if len(expect) != 0 {
		panic(fmt.Sprintf("scheduling fault: did not find event %s", expect[0]))
	}
	//fmt.Println("expect ok")
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

func runProcessList(t *testing.T, pspec string) []string {
	resList := runProcessList2(t, pspec)
	var out []string
	for _, res := range resList {
		fmt.Println("*** res", res.err)
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
			args := strings.Fields(plines[k])
			args = append(args, "--id")
			args = append(args, fmt.Sprintf("%d", k))
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

func testLookup(t *testing.T, pspec string) []string {
	debugPrintTrace := false

	slist := runProcessList(t, pspec)
	verify_NEW_FOUND_count(t, slist)

	addEvents(slist)

	if debugPrintTrace {
		printList(slist)
		fmt.Println("# ok")
	}

	return slist
}

func printList(slist []string) {
	for _, s := range slist {
		fmt.Println(s)
		fmt.Println("===")
	}
}

func verify_NEW_FOUND_count(t *testing.T, slist []string) {
	nproc := len(slist)
	m := make(map[string]int)
	for _, s := range slist {
		n := 0
		for _, line := range strings.Split(s, "\n") {
			switch line {
			case "NEW":
				fallthrough
			case "FOUND":
				m[line] = m[line] + 1
				n += 1
			}
		}
		if n != 1 {
			t.Fatalf("expected only one of NEW or FOUND, got %d events", n)
		}
	}
	if m["NEW"] != 1 {
		t.Fatalf("expected only one NEW event, got %d", m["NEW"])
	}
	if m["NEW"]+m["FOUND"] != nproc {
		t.Fatalf("result mismatch %d + %d != %d", m["NEW"], m["FOUND"], nproc)
	}
}

func hasLine(res string, x string) bool {
	for _, line := range strings.Split(res, "\n") {
		if line == x {
			return true
		}
	}
	return false
}

func addEvents(rlist []string) {
	//
	// when we create file with os.O_CREATE we (the caller) can't know if the file
	// was found or if we created it (when many processes P are racing)
	// however, if we first check if the file exists, then we can deduce what P created it
	// if all P say 'lockfile exists' except one => we can say the remaining P must have created the file
	//
	nfound := 0
	k := -1
	for i, res := range rlist {
		if hasLine(res, "lockfile exists") {
			nfound += 1
		} else {
			k = i
		}
	}
	if nfound == len(rlist)-1 {
		s := rlist[k]
		k2 := strings.Index(s, "\n")
		if k2 > 0 {
			rlist[k] = s[0:k2+1] + "lockfile created\n" + s[k2+1:]
		} else {
			rlist[k] = "lockfile created\n" + s
		}
	}
}

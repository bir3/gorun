package cache2

import (
	"fmt"
	"path"
	"strings"
	"testing"
)

/*
test only one P calls f:

	run multiple processes with f that creates random file in dir
	pass if count of files is 1

test only one P completes f even if some P fails

	run multiple processes with that creates random file in dir
	but before creating file, fail one or more
	pass if count of files is 1
*/

func TestOnceMultiprocessP2(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	m := map[string]any{"tmpdir64": str2base64(d)}
	ts := template2str(`
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
`, m)

	testOnce(t, ts, d, returnError)
}
func TestOnceMultiprocessP2Fail(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	m := map[string]any{"tmpdir64": str2base64(d)}
	ts := template2str(`
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}},delayStart=1
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}},fail=1
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}},fail=1
`, m)

	testOnce(t, ts, d, func(resError CmdResult) error {
		if strings.Contains(resError.out, "failing as requested") {
			fmt.Println("*** found valid panic error")
			return nil
		}
		fmt.Println("***", resError.out)
		return resError.err
	})
	n := countFiles(d, "partial-")
	if n != 3 {
		t.Fatalf("expected 2 partial files but got %d", n)
	}
}

func TestOnceMultiprocessP5(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	m := map[string]any{"tmpdir64": str2base64(d)}
	ts := template2str(`
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
`, m)
	testOnce(t, ts, d, returnError)

}

func TestOnceMultiprocessP33(t *testing.T) {
	t.Parallel()
	d := t.TempDir()

	pcount := 33
	r := strings.Split(strings.Repeat("*", pcount), "") // ["*", "*", ...]
	m := map[string]any{"tmpdir64": str2base64(d), "r": r}
	ts := template2str(`
	{{range .r}}
	{{$.exe}} --entry=onceSubprocess --map=tmpdir64={{$.tmpdir64}}
	{{end}}
`, m)
	testOnce(t, ts, d, returnError)

}

func returnError(resError CmdResult) error { return resError.err }

func testOnce(t *testing.T, ts string, d string, inspectError func(res CmdResult) error) {

	var out []string
	resList := runProcessList2(t, ts)
	for _, res := range resList {
		if res.err != nil {
			err := inspectError(res)
			if err != nil {
				t.Fatalf("%s", err)
			}
		}
		out = append(out, res.out)
	}

	n := countFiles(d, "create-")
	debug := false
	if debug {
		for _, s := range out {
			fmt.Println("* TestOnce: out", s)
		}
	}
	if n != 1 {
		t.Fatalf("expected single run (once) but actual runs were %d", n)
	}
}

func findSplitPath(xpath string, x string) bool {
	var dirname, basename string = xpath, ""
	for dirname != "" {
		if dirname[len(dirname)-1:] == "/" {
			dirname = dirname[0 : len(dirname)-1]
		}
		old := dirname
		dirname, basename = path.Split(dirname)
		if old == dirname {
			break
		}
		if basename == x {
			return true
		}
	}
	return false
}

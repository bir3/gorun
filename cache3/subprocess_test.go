package cache2

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testEntry = flag.String("entry", "", "child process entry-point")
var testId = flag.String("id", "", "child process id")
var testDelay = flag.String("delay", "", "delay map for process")
var testMap = flag.String("map", "", "key-value map")
var testExpire = flag.Int("expire_ms", 0, "expire age in milliseconds")

func TestMain(m *testing.M) {
	// In some build systems, notably Blaze, flag.Parse is called before TestMain,
	// in violation of the TestMain contract, making this second call a no-op.
	flag.Parse()
	switch *testEntry {
	case "":
		status := m.Run()
		if status == 0 {
			//err := _cleanTmpDir()
			var err error
			if err != nil {
				fmt.Printf("ERROR: %s\n", err)
				status = 18
			}
		}
		os.Exit(status) // normal case
	case "lookupSubprocess":
		lookupSubprocess(*testId, *testDelay, createMap(*testMap))
		/*
			case "deleteSubprocess":
				deleteSubprocess(*testId, *testDelay, *testExpire, createMap(*testMap))
			case "onceSubprocess":
				onceSubprocess(*testId, createMap(*testMap))
		*/
	default:
		log.Fatalf("unknown entry point: %s", *testEntry)
	}
}

func createMap(s string) map[string]string {
	m := make(map[string]string)
	// key=20,key2=30,...
	if s != "" {
		for _, x := range strings.Split(s, ",") {
			key, value, found := strings.Cut(x, "=")
			if !found {
				panic(fmt.Sprintf("missing equal sign: %q", x))
			}

			if strings.TrimSpace(key) != key || len(strings.TrimSpace(key)) == 0 {
				panic(fmt.Sprintf("bad key %q", key))
			}
			if strings.TrimSpace(value) != value {
				panic(fmt.Sprintf("bad value %q", value))
			}

			m[key] = value
		}
	}
	return m
}

func num(m map[string]string, key string) int {
	valueInt, err := strconv.Atoi(m[key])
	if err != nil {
		panic(fmt.Sprintf("key %s has value %s - can't convert to integer", key, m[key]))
	}
	return valueInt
}

func get(m map[string]string, key string) string {
	val := m[key]
	if strings.HasSuffix(key, "64") {
		d, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			panic(fmt.Sprintf("key %s has bad base64 value %s", key, val))
		}
		return string(d)
	}
	return val
}

/*
	started by runProcessList via TestMain


func onceSubprocess(id string, m map[string]string) {
	tmpdir := get(m, "tmpdir64")
	lockfile := tmpdir + ".lock"

	if _, found := m["delayStart"]; found {
		fmt.Println("*** delayStart")
		time.Sleep(25 * time.Millisecond)
	}

	err := OnceMultiprocess(lockfile, func() error {
		mustWriteFile(fmt.Sprintf("%s/partial-%s", tmpdir, mustUid()), "abc")
		time.Sleep(100 * time.Millisecond)
		if _, found := m["fail"]; found {
			return fmt.Errorf("failing as requested")
		}
		mustWriteFile(fmt.Sprintf("%s/create-%s", tmpdir, mustUid()), "abc")
		return nil
	})
	if err != nil {
		panic(err)
	}

}
*/

func lookupSubprocess(id string, delay string, m map[string]string) {
	fmt.Printf("lookupTest id=%s\n", id)
	cacheDir := get(m, "tmpdir64")

	config, err := NewConfig(cacheDir, 10*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(8)
	}
	/*
		config.testLog = id
		config.testDelayMs = make(map[string]int)
		config.setDelay(delay)
	*/
	found := true
	create := func(outdir string) error {
		found = false
		return nil
	}
	objDir, err := config.Lookup("a7a7", create)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(8)
	}
	if !path.IsAbs(objDir) {
		panic(fmt.Sprintf("Lookup did not return abs path, got objDir=%s", objDir))
	}
	if found {
		fmt.Println("FOUND")
		/*
			f := path.Join(objDir, "data.txt")
			b, err := os.ReadFile(f)
			if err != nil {
				panic(fmt.Sprintf("read cache object failed, file %s error %s", f, err.Error()))
			}
			if string(b) != "abc" {
				panic(fmt.Sprintf("read cache object did not return expected data %q but got %q", "abc", string(b)))
			}
		*/
	} else {
		fmt.Println("NEW")
	}
	fmt.Println("objDir", objDir)
}

/*
func deleteSubprocess(id string, delay string, expire_ms int, m map[string]string) {
	fmt.Printf("deleteTest id=%s\n", id)
	cacheDir := get(m, "tmpdir64")
	config, err := Create(cacheDir)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(10)
	}
	config.testDelayMs = make(map[string]int)
	config.setDelay(delay)

	config.maxAge = time.Millisecond * time.Duration(expire_ms)
	config.testLog = "99-delete"

	//time.Sleep(time.Millisecond * 10)
	config.DeleteExpiredObjects()
}
*/

func mustWriteFile(filename string, data string) {
	err := os.WriteFile(filename, []byte(data), 0666)
	if err != nil {
		panic(err)
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

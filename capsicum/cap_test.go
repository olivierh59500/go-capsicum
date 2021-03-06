// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// FIXME: mostly just a copy of syscall_freebsd_test.go

package capsicum_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/benlaurie/go-capsicum/capsicum"
	"golang.org/x/sys/unix"
)

// FIXME: Infrastructure for launching tests in subprocesses stolen from openbsd_test.go - refactor?
// testCmd generates a proper command that, when executed, runs the test
// corresponding to the given key.

type testProc struct {
	fn      func()                    // should always exit instead of returning
	arg     func(t *testing.T) string // generate argument for test
	cleanup func(arg string) error    // for instance, delete coredumps from testing pledge
	success bool                      // whether zero-exit means success or failure
}

var (
	testProcs = map[string]testProc{}
	procName  = ""
	procArg   = ""
)

const (
	optName = "sys-unix-internal-procname"
	optArg  = "sys-unix-internal-arg"
)

func init() {
	flag.StringVar(&procName, optName, "", "internal use only")
	flag.StringVar(&procArg, optArg, "", "internal use only")

}

func testCmd(procName string, procArg string) (*exec.Cmd, error) {
	exe, err := filepath.Abs(os.Args[0])
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "-"+optName+"="+procName, "-"+optArg+"="+procArg)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd, nil
}

// ExitsCorrectly is a comprehensive, one-line-of-use wrapper for testing
// a testProc with a key.
func ExitsCorrectly(t *testing.T, procName string) {
	s := testProcs[procName]
	arg := "-"
	if s.arg != nil {
		arg = s.arg(t)
	}
	c, err := testCmd(procName, arg)
	defer func(arg string) {
		if err := s.cleanup(arg); err != nil {
			t.Fatalf("Failed to run cleanup for %s %s %#v", procName, err, err)
		}
	}(arg)
	if err != nil {
		t.Fatalf("Failed to construct command for %s", procName)
	}
	if (c.Run() == nil) != s.success {
		result := "succeed"
		if !s.success {
			result = "fail"
		}
		t.Fatalf("Process did not %s when it was supposed to", result)
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	if procName != "" {
		t := testProcs[procName]
		t.fn()
		os.Stderr.WriteString("test function did not exit\n")
		if t.success {
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

// end of infrastructure

const testfile = "gocapmodetest"
const testfile2 = testfile + "2"

func CapEnterTest() {
	_, err := os.OpenFile(path.Join(procArg, testfile), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(fmt.Sprintf("OpenFile: %s", err))
	}

	err = capsicum.CapEnter()
	if err != nil {
		panic(fmt.Sprintf("CapEnter: %s", err))
	}

	_, err = os.OpenFile(path.Join(procArg, testfile2), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		panic("OpenFile works!")
	}
	if err.(*os.PathError).Err != capsicum.ECAPMODE {
		panic(fmt.Sprintf("OpenFile failed wrong: %s %#v", err, err))
	}
	os.Exit(0)
}

func makeTempDir(t *testing.T) string {
	d, err := ioutil.TempDir("", "go_openat_test")
	if err != nil {
		t.Fatalf("TempDir failed: %s", err)
	}
	return d
}

func removeTempDir(arg string) error {
	err := os.RemoveAll(arg)
	if err != nil && err.(*os.PathError).Err == unix.ENOENT {
		return nil
	}
	return err
}

func init() {
	testProcs["cap_enter"] = testProc{
		CapEnterTest,
		makeTempDir,
		removeTempDir,
		true,
	}
}

func TestCapEnter(t *testing.T) {
	ExitsCorrectly(t, "cap_enter")
}

func OpenatTest() {
	f, err := os.Open(procArg)
	if err != nil {
		panic(err)
	}

	err = capsicum.CapEnter()
	if err != nil {
		panic(fmt.Sprintf("CapEnter: %s", err))
	}

	fxx, err := unix.Openat(int(f.Fd()), "xx", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	unix.Close(fxx)

	// Also test OpenFileAt
	f2, err := capsicum.OpenFileAt(f, "yy", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	f2.Close()

	// The right to open BASE/xx is not ambient
	_, err = os.OpenFile(procArg+"/xx", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		panic("OpenFile succeeded")
	}
	if err.(*os.PathError).Err != capsicum.ECAPMODE {
		panic(fmt.Sprintf("OpenFile failed wrong: %s %#v", err, err))
	}

	// Can't make a new directory either
	err = os.Mkdir(procArg+"2", 0777)
	if err == nil {
		panic("MKdir succeeded")
	}
	// FIXME: mkdir on Linux currently incorrectly returns EPERM
	if err.(*os.PathError).Err != capsicum.ECAPMODE && err.(*os.PathError).Err != unix.EPERM {
		panic(fmt.Sprintf("Mkdir failed wrong: %s %#v", err, err))
	}

	// Remove all caps except read and lookup.
	r, err := capsicum.CapRightsInit(capsicum.CAP_READ, capsicum.CAP_LOOKUP)
	if err != nil {
		panic(fmt.Sprintf("CapRightsInit failed: %s %#v", err, err))
	}
	err = capsicum.CapRightsLimit(f, r)
	if err != nil {
		panic(fmt.Sprintf("CapRightsLimit failed: %s %#v", err, err))
	}

	// Check we can get the rights back again
	r, err = capsicum.CapRightsGet(f)
	if err != nil {
		panic(fmt.Sprintf("CapRightsGet failed: %s %#v", err, err))
	}
	b, err := capsicum.CapRightsIsSet(r, capsicum.CAP_READ, capsicum.CAP_LOOKUP)
	if err != nil {
		panic(fmt.Sprintf("CapRightsIsSet failed: %s %#v", err, err))
	}
	if !b {
		panic(fmt.Sprintf("Unexpected rights"))
	}
	b, err = capsicum.CapRightsIsSet(r, capsicum.CAP_READ, capsicum.CAP_LOOKUP, capsicum.CAP_WRITE)
	if err != nil {
		panic(fmt.Sprintf("CapRightsIsSet failed: %s %#v", err, err))
	}
	if b {
		panic(fmt.Sprintf("Unexpected rights (2)"))
	}

	// Can no longer create a file
	_, err = unix.Openat(int(f.Fd()), "xx2", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		panic("Openat succeeded")
	}
	if err != capsicum.ENOTCAPABLE {
		panic(fmt.Sprintf("OpenFileAt failed wrong: %s %#v", err, err))
	}

	// But can read an existing one
	_, err = unix.Openat(int(f.Fd()), "xx", os.O_RDONLY, 0666)
	if err != nil {
		panic(fmt.Sprintf("Openat failed: %s %#v", err, err))
	}

	os.Exit(0)
}

func init() {
	testProcs["openat"] = testProc{
		OpenatTest,
		makeTempDir,
		removeTempDir,
		true,
	}
}

func TestOpenat(t *testing.T) {
	ExitsCorrectly(t, "openat")
}

func TestCapRightsSetAndClear(t *testing.T) {
	r, err := capsicum.CapRightsInit(capsicum.CAP_READ, capsicum.CAP_WRITE, capsicum.CAP_PDWAIT)
	if err != nil {
		t.Fatalf("CapRightsInit failed: %s", err)
	}

	err = capsicum.CapRightsSet(r, capsicum.CAP_EVENT, capsicum.CAP_LISTEN)
	if err != nil {
		t.Fatalf("CapRightsSet failed: %s", err)
	}

	b, err := capsicum.CapRightsIsSet(r, capsicum.CAP_READ, capsicum.CAP_WRITE, capsicum.CAP_PDWAIT, capsicum.CAP_EVENT, capsicum.CAP_LISTEN)
	if err != nil {
		t.Fatalf("CapRightsIsSet failed: %s", err)
	}
	if !b {
		t.Fatalf("Wrong rights set")
	}

	err = capsicum.CapRightsClear(r, capsicum.CAP_READ, capsicum.CAP_PDWAIT)
	if err != nil {
		t.Fatalf("CapRightsClear failed: %s", err)
	}

	b, err = capsicum.CapRightsIsSet(r, capsicum.CAP_READ, capsicum.CAP_WRITE, capsicum.CAP_PDWAIT, capsicum.CAP_EVENT, capsicum.CAP_LISTEN)
	if err != nil {
		t.Fatalf("CapRightsIsSet failed: %s", err)
	}
	if b {
		t.Fatalf("Wrong rights set")
	}

	b, err = capsicum.CapRightsIsSet(r, capsicum.CAP_WRITE, capsicum.CAP_EVENT, capsicum.CAP_LISTEN)
	if err != nil {
		t.Fatalf("CapRightsIsSet failed: %s", err)
	}
	if !b {
		t.Fatalf("Wrong rights set")
	}
}

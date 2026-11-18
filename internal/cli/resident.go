package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// A resident check-content server keeps one compiled corpus for a root and
// answers over a Unix socket, so a hook that runs once per edit does not pay
// for loading and compiling every rule on every call. A connection is exactly
// one `check-content --batch` run against that corpus: a header line, the batch
// records, and the batch's own output followed by a trailer carrying its exit
// code and stderr. The corpus reloads when .formwork changes, as `serve --mcp`
// does.

const (
	residentIdleDefault = 15 * time.Minute
	residentDialTimeout = 200 * time.Millisecond
	residentConnTimeout = 2 * time.Minute
	// residentSocketPathMax keeps under the 104-byte sun_path of macOS.
	residentSocketPathMax = 100
)

type residentHeader struct {
	Version     string `json:"version"`
	Root        string `json:"root"`
	AllFindings bool   `json:"all_findings"`
}

// residentTrailer ends every answer. Refused means this server cannot answer
// for the request at all, so the client evaluates locally instead.
type residentTrailer struct {
	Exit    int    `json:"exit"`
	Stderr  string `json:"stderr"`
	Refused bool   `json:"refused,omitempty"`
}

// residentStart starts a server for the next call. Unit tests replace it,
// because the real one execs the running binary, which under `go test` is the
// test binary.
var residentStart = startResidentServer

func canonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// residentSocketPath names the socket for one engine version and one root, so
// two versions or two worktrees never share a corpus. The name is a hash of
// both, so its length does not depend on how long a version string is.
func residentSocketPath(dir, root string) (string, error) {
	canon, err := canonicalRoot(root)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(displayVersion() + "\x00" + canon))
	path := filepath.Join(dir, fmt.Sprintf("formwork-%x.sock", sum[:12]))
	if len(path) > residentSocketPathMax {
		return "", fmt.Errorf("socket path %s is longer than %d bytes", path, residentSocketPathMax)
	}
	return path, nil
}

func serveSocket(corpus *checkFileCorpus, path string, idle time.Duration, stderr io.Writer) int {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintln(stderr, "formwork: resident server:", err)
		return 2
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintln(stderr, "formwork: resident server:", err)
		return 2
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return 0
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "formwork: resident server:", err)
		return 2
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintln(stderr, "formwork: resident server:", err)
		return 2
	}
	// Close unlinks the socket: a UnixListener removes the path it created.
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		fmt.Fprintln(stderr, "formwork: resident server:", err)
		return 2
	}
	idleTimer := time.AfterFunc(idle, func() { listener.Close() })
	defer idleTimer.Stop()
	// An answer in flight when the idle period ends still completes: the
	// process does not exit until every accepted connection is answered.
	var serial sync.Mutex
	var inFlight sync.WaitGroup
	defer inFlight.Wait()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return 0
			}
			fmt.Fprintln(stderr, "formwork: resident server:", err)
			return 2
		}
		idleTimer.Reset(idle)
		inFlight.Add(1)
		go func() {
			defer inFlight.Done()
			serial.Lock()
			defer serial.Unlock()
			corpus.answer(conn)
		}()
	}
}

func (c *checkFileCorpus) answer(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(residentConnTimeout))
	reader := bufio.NewReader(conn)
	finish := func(t residentTrailer) { emitJSONLine(conn, io.Discard, t) }
	line, err := reader.ReadBytes('\n')
	var header residentHeader
	if err != nil || json.Unmarshal(line, &header) != nil {
		finish(residentTrailer{Exit: 2, Refused: true, Stderr: "formwork: resident server: unreadable request header\n"})
		return
	}
	served, err := canonicalRoot(c.root)
	// The version check cannot be pinned by a test: one test binary has one
	// version. The socket name carries the version too, so a client only
	// reaches a server of another version through a hand-built path.
	if err != nil || header.Version != displayVersion() || header.Root != served {
		finish(residentTrailer{Exit: 2, Refused: true, Stderr: fmt.Sprintf("formwork: resident server for %s at %s cannot answer for %s at %s\n", served, displayVersion(), header.Root, header.Version)})
		return
	}
	cfg, err := c.load()
	if err != nil {
		finish(residentTrailer{Exit: 2, Stderr: fmt.Sprintln("formwork:", err)})
		return
	}
	var out, errOut bytes.Buffer
	code := runCheckContentBatch(cfg, c.root, reader, &out, &errOut, header.AllFindings)
	if _, err := conn.Write(out.Bytes()); err != nil {
		return
	}
	finish(residentTrailer{Exit: code, Stderr: errOut.String()})
}

// checkContentViaResident answers from the resident server for root under dir.
// ok is false when no server could answer; the caller then evaluates locally,
// and a server is started for the next call.
func checkContentViaResident(dir, root string, batch bool, path string, allFindings bool, input []byte, stdout, stderr io.Writer) (code int, ok bool) {
	socket, err := residentSocketPath(dir, root)
	if err != nil {
		return 0, false
	}
	canon, err := canonicalRoot(root)
	if err != nil {
		return 0, false
	}
	dialed, err := net.DialTimeout("unix", socket, residentDialTimeout)
	if err != nil {
		residentStart(socket, canon)
		return 0, false
	}
	conn, isUnix := dialed.(*net.UnixConn)
	if !isUnix {
		dialed.Close()
		return 0, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(residentConnTimeout))
	if emitJSONLine(conn, io.Discard, residentHeader{Version: displayVersion(), Root: canon, AllFindings: allFindings}) != 0 {
		return 0, false
	}
	if batch {
		_, err = conn.Write(input)
	} else {
		content := string(input)
		if emitJSONLine(conn, io.Discard, checkContentBatchRecord{Path: path, Content: &content}) != 0 {
			return 0, false
		}
	}
	if err != nil || conn.CloseWrite() != nil {
		return 0, false
	}
	answer, err := io.ReadAll(conn)
	if err != nil {
		return 0, false
	}
	lines := bytes.Split(bytes.TrimSuffix(answer, []byte("\n")), []byte("\n"))
	var trailer residentTrailer
	if json.Unmarshal(lines[len(lines)-1], &trailer) != nil || trailer.Refused {
		return 0, false
	}
	docs := lines[:len(lines)-1]
	if batch {
		for _, doc := range docs {
			stdout.Write(append(doc, '\n'))
		}
		io.WriteString(stderr, trailer.Stderr)
		return trailer.Exit, true
	}
	io.WriteString(stderr, strings.Replace(trailer.Stderr, "formwork: check-content batch line 1: ", "formwork: ", 1))
	if len(docs) == 0 {
		return trailer.Exit, true
	}
	var doc checkContentDocument
	if len(docs) != 1 || json.Unmarshal(docs[0], &doc) != nil {
		return 0, false
	}
	if code := emitJSON(stdout, stderr, doc); code != 0 {
		return code, true
	}
	return trailer.Exit, true
}

// startResidentServer starts this binary as the server for socket, detached
// from the caller's session, and does not wait for it.
func startResidentServer(socket, root string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "serve", "--socket", socket, "-C", root)
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

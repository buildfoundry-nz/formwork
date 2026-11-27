package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const residentRules = `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope: {include: ['**/*.txt']}
    params: {pattern: TODO}
    cure: resolve the item
`

func residentRepo(t *testing.T, rules string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{".formwork/formwork.yaml": "version: 1\n", ".formwork/rules/rules.yaml": rules} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// residentDir is short on purpose: a macOS test temp dir can push a socket
// path past the 104-byte sun_path limit.
func residentDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "fwres")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// startResident serves root on socket and returns a channel carrying the
// server's exit code. The server stops when idle elapses.
func startResident(t *testing.T, root, socket string, idle time.Duration) <-chan int {
	t.Helper()
	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		done <- Run([]string{"serve", "--socket", socket, "-C", root, "--idle", idle.String()}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if conn, err := net.Dial("unix", socket); err == nil {
			conn.Close()
			return done
		}
		if time.Now().After(deadline) {
			t.Fatalf("resident server never listened on %s", socket)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func noStart(t *testing.T) {
	t.Helper()
	prev := residentStart
	residentStart = func(socket, root string) { t.Errorf("started a server for %s while one should answer", socket) }
	t.Cleanup(func() { residentStart = prev })
}

var elapsedField = regexp.MustCompile(`"elapsed_ms": ?[0-9]+`)

func sameAnswer(t *testing.T, name string, localCode int, localOut, localErr string, code int, out, errOut string) {
	t.Helper()
	if code != localCode || elapsedField.ReplaceAllString(out, "") != elapsedField.ReplaceAllString(localOut, "") || errOut != localErr {
		t.Fatalf("%s: resident answer differs from local\nlocal  %d %q %q\nserver %d %q %q", name, localCode, localOut, localErr, code, out, errOut)
	}
}

func runLocal(args []string, input string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCheckContentServerAnswersAsLocalEvaluationDoes(t *testing.T) {
	root := residentRepo(t, residentRules)
	dir := residentDir(t)
	socket, err := residentSocketPath(dir, root)
	if err != nil {
		t.Fatal(err)
	}
	startResident(t, root, socket, 5*time.Second)
	noStart(t)
	for _, tc := range []struct{ name, path, input string }{
		{"finding", "a.txt", "TODO\n"},
		{"clean", "a.txt", "done\n"},
		{"outside root", "../a.txt", "TODO\n"},
	} {
		localCode, localOut, localErr := runLocal([]string{"check-content", "-C", root, "--path", tc.path}, tc.input)
		var stdout, stderr bytes.Buffer
		code, ok := checkContentViaResident(dir, root, false, tc.path, false, []byte(tc.input), &stdout, &stderr)
		if !ok {
			t.Fatalf("%s: the running server did not answer", tc.name)
		}
		sameAnswer(t, tc.name, localCode, localOut, localErr, code, stdout.String(), stderr.String())
	}
	batch := `{"path":"a.txt","content":"TODO\n"}` + "\n" + `{"path":"b.txt","content":"fine\n"}` + "\n"
	localCode, localOut, localErr := runLocal([]string{"check-content", "-C", root, "--batch", "--all-findings"}, batch)
	var stdout, stderr bytes.Buffer
	code, ok := checkContentViaResident(dir, root, true, "", true, []byte(batch), &stdout, &stderr)
	if !ok {
		t.Fatal("batch: the running server did not answer")
	}
	sameAnswer(t, "batch", localCode, localOut, localErr, code, stdout.String(), stderr.String())
}

func TestCheckContentServerMissStartsOneAndEvaluatesLocally(t *testing.T) {
	root := residentRepo(t, residentRules)
	dir := residentDir(t)
	var started []string
	prev := residentStart
	residentStart = func(socket, served string) { started = append(started, socket, served) }
	t.Cleanup(func() { residentStart = prev })
	localCode, localOut, localErr := runLocal([]string{"check-content", "-C", root, "--path", "a.txt"}, "TODO\n")
	code, out, errOut := runLocal([]string{"check-content", "-C", root, "--server", dir, "--path", "a.txt"}, "TODO\n")
	sameAnswer(t, "miss", localCode, localOut, localErr, code, out, errOut)
	socket, _ := residentSocketPath(dir, root)
	canon, _ := canonicalRoot(root)
	if len(started) != 2 || started[0] != socket || started[1] != canon {
		t.Fatalf("started = %q, want a server for %s at %s", started, canon, socket)
	}
}

func TestResidentServerRefusesARootItDoesNotServe(t *testing.T) {
	served := residentRepo(t, residentRules)
	other := residentRepo(t, "rules: []\n")
	dir := residentDir(t)
	socket, err := residentSocketPath(dir, other)
	if err != nil {
		t.Fatal(err)
	}
	startResident(t, served, socket, 5*time.Second)
	noStart(t)
	var stdout, stderr bytes.Buffer
	if _, ok := checkContentViaResident(dir, other, false, "a.txt", false, []byte("TODO\n"), &stdout, &stderr); ok {
		t.Fatalf("a server for another root answered: %q %q", stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("a refused answer leaked output: %q %q", stdout.String(), stderr.String())
	}
}

func TestResidentServerSecondInstanceLeavesTheFirstServing(t *testing.T) {
	root := residentRepo(t, residentRules)
	dir := residentDir(t)
	socket, _ := residentSocketPath(dir, root)
	startResident(t, root, socket, 5*time.Second)
	second := make(chan int, 1)
	go func() {
		second <- Run([]string{"serve", "--socket", socket, "-C", root, "--idle", "5s"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	}()
	select {
	case code := <-second:
		if code != 0 {
			t.Fatalf("second instance exit = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a second server on a held socket kept running")
	}
	noStart(t)
	if _, ok := checkContentViaResident(dir, root, false, "a.txt", false, []byte("TODO\n"), &bytes.Buffer{}, &bytes.Buffer{}); !ok {
		t.Fatal("the first server stopped answering after a second instance started")
	}
}

// A server started on a miss may never be asked anything: its first idle
// period must end it too, so this waits for the socket without connecting.
func TestResidentServerExitsWhenIdleAndRemovesItsSocket(t *testing.T) {
	root := residentRepo(t, residentRules)
	dir := residentDir(t)
	socket, _ := residentSocketPath(dir, root)
	done := make(chan int, 1)
	go func() {
		done <- Run([]string{"serve", "--socket", socket, "-C", root, "--idle", "300ms"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	}()
	listened := false
	deadline := time.After(5 * time.Second)
	for {
		select {
		case code := <-done:
			if code != 0 || !listened {
				t.Fatalf("idle exit = %d after listening %v, want 0 after listening", code, listened)
			}
			if _, err := os.Stat(socket); !os.IsNotExist(err) {
				t.Fatalf("socket left behind: %v", err)
			}
			return
		case <-deadline:
			t.Fatal("server did not exit when idle")
		case <-time.After(5 * time.Millisecond):
			if _, err := os.Stat(socket); err == nil {
				listened = true
			}
		}
	}
}

func TestResidentServerAnswersWithTheChangedRules(t *testing.T) {
	root := residentRepo(t, residentRules)
	dir := residentDir(t)
	socket, _ := residentSocketPath(dir, root)
	startResident(t, root, socket, 5*time.Second)
	noStart(t)
	ask := func() (int, string) {
		var stdout bytes.Buffer
		code, ok := checkContentViaResident(dir, root, false, "a.txt", false, []byte("FIXME\n"), &stdout, &bytes.Buffer{})
		if !ok {
			t.Fatal("the running server did not answer")
		}
		return code, stdout.String()
	}
	if code, out := ask(); code != 0 {
		t.Fatalf("before the change: code %d %s", code, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".formwork", "rules", "rules.yaml"), []byte(strings.Replace(residentRules, "TODO", "FIXME", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, out := ask()
		if code == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server kept answering from the old rules: code %d %s", code, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestResidentServerAnswerEndsWithItsTrailer(t *testing.T) {
	root := residentRepo(t, residentRules)
	dir := residentDir(t)
	socket, _ := residentSocketPath(dir, root)
	startResident(t, root, socket, 5*time.Second)
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v err %v, want 0600", info.Mode(), err)
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	canon, _ := canonicalRoot(root)
	header, _ := json.Marshal(residentHeader{Version: displayVersion(), Root: canon})
	conn.Write(append(header, '\n'))
	conn.Write([]byte(`{"path":"a.txt","content":"TODO\n"}` + "\n"))
	conn.(*net.UnixConn).CloseWrite()
	var lines []string
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	var trailer residentTrailer
	if len(lines) != 2 || json.Unmarshal([]byte(lines[1]), &trailer) != nil || trailer.Exit != 1 || trailer.Refused {
		t.Fatalf("answer = %q", lines)
	}
}

func TestServeRefusesAmbiguousInvocations(t *testing.T) {
	root := residentRepo(t, residentRules)
	for _, args := range [][]string{
		{"serve", "-C", root},
		{"serve", "--mcp", "--socket", "/tmp/x.sock", "-C", root},
		{"serve", "--socket", "/tmp/x.sock", "--idle", "0s", "-C", root},
	} {
		if code, _, _ := runLocal(args, ""); code != 2 {
			t.Fatalf("%q exit = %d, want 2", args, code)
		}
	}
}

func TestResidentServerFinishesAnAnswerInFlightWhenIdleEnds(t *testing.T) {
	root := residentRepo(t, residentRules)
	dir := residentDir(t)
	socket, _ := residentSocketPath(dir, root)
	done := startResident(t, root, socket, 200*time.Millisecond)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	canon, _ := canonicalRoot(root)
	header, _ := json.Marshal(residentHeader{Version: displayVersion(), Root: canon})
	conn.Write(append(header, '\n'))
	select {
	case code := <-done:
		t.Fatalf("server exited (%d) with an answer in flight", code)
	case <-time.After(600 * time.Millisecond):
	}
	conn.Write([]byte(`{"path":"a.txt","content":"TODO\n"}` + "\n"))
	conn.(*net.UnixConn).CloseWrite()
	answer, err := io.ReadAll(conn)
	if err != nil || !bytes.Contains(answer, []byte(`"exit":1`)) {
		t.Fatalf("answer = %q err %v", answer, err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not exit after its last answer")
	}
}

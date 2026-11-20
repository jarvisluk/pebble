package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// lineProc is a long-lived child process speaking newline-delimited JSON on
// stdin/stdout. Lines arrive on lines, which is closed when the process exits.
type lineProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	done   chan struct{}
	stderr *tailWriter
	wmu    sync.Mutex
}

func startLineProc(cmd *exec.Cmd) (*lineProc, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &lineProc{cmd: cmd, stdin: stdin, lines: make(chan []byte, 1024), done: make(chan struct{}), stderr: &tailWriter{}}
	cmd.Stderr = p.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	go func() {
		r := bufio.NewReaderSize(stdout, 64*1024)
		for {
			// Split on LF only: JSON strings may contain U+2028/U+2029.
			line, err := r.ReadBytes('\n')
			if len(strings.TrimSpace(string(line))) > 0 {
				p.lines <- line
			}
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
		close(p.lines)
		close(p.done)
	}()
	return p, nil
}

func (p *lineProc) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

func (p *lineProc) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *lineProc) kill() {
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
}

// exitDetail describes why the process ended, from its stderr tail.
func (p *lineProc) exitDetail() string {
	if s := strings.TrimSpace(p.stderr.String()); s != "" {
		return s
	}
	return "process exited"
}

// tailWriter keeps the last 4 KiB written to it.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *tailWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	if n := len(w.buf); n > 4096 {
		w.buf = w.buf[n-4096:]
	}
	return len(b), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// splitModel splits "provider/model..." at the first slash.
func splitModel(m string) (provider, id string) {
	provider, id, _ = strings.Cut(m, "/")
	return provider, id
}

func newUUID() string {
	h := randHex(16)
	// version 4, variant 10
	b := []byte(h)
	b[12] = '4'
	b[16] = "89ab"[int(b[16])%4]
	return string(b[0:8]) + "-" + string(b[8:12]) + "-" + string(b[12:16]) + "-" + string(b[16:20]) + "-" + string(b[20:32])
}

func procEnv(cmd *exec.Cmd, opts RunOptions) []string {
	env := cmd.Environ()
	for k, v := range opts.Bot.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range opts.ExtraEnv {
		env = append(env, k+"="+v)
	}
	return env
}

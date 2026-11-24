package harness

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakePoolProc struct {
	aliveFlag atomic.Bool
	killed    atomic.Bool
}

func newFakePoolProc() *fakePoolProc {
	p := &fakePoolProc{}
	p.aliveFlag.Store(true)
	return p
}

func (p *fakePoolProc) alive() bool { return p.aliveFlag.Load() }
func (p *fakePoolProc) kill() {
	p.aliveFlag.Store(false)
	p.killed.Store(true)
}

func TestProcPoolDefaultNoIdleReap(t *testing.T) {
	pool := &procPool[*fakePoolProc]{}
	defer pool.close()

	if got := pool.effectiveIdleTTL(); got != 0 {
		t.Fatalf("expected default idleTTL=0 (no reaping), got %v", got)
	}
	proc, release, err := pool.acquire("b1", nil, func() (*fakePoolProc, error) {
		return newFakePoolProc(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	release()
	pool.reapIdle(time.Now().Add(24 * time.Hour))
	if proc.killed.Load() {
		t.Fatalf("process must not be reaped when idle reaping is disabled by default")
	}
}

func TestProcPoolIdleReapRespectsInUseAndTTL(t *testing.T) {
	ttl := 50 * time.Millisecond
	pool := &procPool[*fakePoolProc]{
		idleTTL:      &ttl,
		reapInterval: 10 * time.Millisecond,
	}
	defer pool.close()

	pBusy, releaseBusy, err := pool.acquire("busy", nil, func() (*fakePoolProc, error) {
		return newFakePoolProc(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pIdle, releaseIdle, err := pool.acquire("idle", nil, func() (*fakePoolProc, error) {
		return newFakePoolProc(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	releaseIdle()

	deadline := time.Now().Add(time.Second)
	for !pIdle.killed.Load() && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if !pIdle.killed.Load() {
		t.Fatalf("expected idle process to be reaped after TTL")
	}
	if pBusy.killed.Load() {
		t.Fatalf("in-use process must never be reaped while active")
	}
	releaseBusy()
}

func TestUpsertMCPConfigAtomicAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "mcp.json")
	opts := RunOptions{
		MCPCommand: []string{"/usr/local/bin/pebble", "mcp"},
		MCPEnv:     map[string]string{"PEBBLE_API_TOKEN": "tok"},
	}

	if err := upsertMCPConfig(path, opts); err != nil {
		t.Fatal(err)
	}
	info1, err := os.Stat(path)
	if err != nil || info1.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 perms, got %v err=%v", info1.Mode().Perm(), err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := upsertMCPConfig(path, opts); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Fatalf("expected identical upsertMCPConfig call to skip rewriting file")
	}
}

func TestReadFrontmatterHead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	content := "---\nname: my-skill\ndescription: short desc\n---\n" + strings.Repeat("x", 8192)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	head, err := readFrontmatterHead(p, frontmatterHeadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(head) != frontmatterHeadBytes {
		t.Fatalf("expected %d bytes read, got %d", frontmatterHeadBytes, len(head))
	}
	name, desc := parseFrontmatter(head)
	if name != "my-skill" || desc != "short desc" {
		t.Fatalf("unexpected frontmatter: name=%q desc=%q", name, desc)
	}
}

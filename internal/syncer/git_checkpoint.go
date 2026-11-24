package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bot-hub/internal/config"
)

const (
	gitIgnoreBegin       = "# pebble-sync:begin"
	gitIgnoreEnd         = "# pebble-sync:end"
	legacyGitIgnoreBegin = "# bot-hub-sync:begin"
	legacyGitIgnoreEnd   = "# bot-hub-sync:end"
)

var managedIgnoreLines = []string{
	".env",
	".env.*",
	"config/bots.json",
	"state/",
	"*.db",
	"*.db-wal",
	"*.db-shm",
	"media/",
	"bin/",
	"**/opencode.json",
	"**/PEBBLE.md",
	"**/BOT_HUB.md",
	"**/CLAUDE.md",
	"**/.agents/skills/pebble-setup/SKILL.md",
	"**/.claude/skills/pebble-setup/SKILL.md",
	"workspaces/*/_shared",
	"!workspaces/_shared/",
	"*.conflict-*",
}

// CheckpointResult summarizes a Git checkpoint commit/push run.
type CheckpointResult struct {
	Committed bool   `json:"committed"`
	Pushed    bool   `json:"pushed"`
	Branch    string `json:"branch"`
	CommitSHA string `json:"commitSha,omitempty"`
	Message   string `json:"message,omitempty"`
}

// PullResult summarizes a Git checkpoint pull and portable apply run.
type PullResult struct {
	Pulled       bool     `json:"pulled"`
	Branch       string   `json:"branch"`
	ConfigChange bool     `json:"configChanged"`
	AddedBots    []string `json:"addedBots,omitempty"`
	UpdatedBots  []string `json:"updatedBots,omitempty"`
}

// EnsureGitIgnore idempotently maintains the managed ignore block in projectRoot/.gitignore.
func EnsureGitIgnore(projectRoot string) error {
	if strings.TrimSpace(projectRoot) == "" {
		return errors.New("projectRoot is required")
	}
	p := filepath.Join(projectRoot, ".gitignore")
	existing := ""
	if raw, err := os.ReadFile(p); err == nil {
		existing = string(raw)
	}

	block := gitIgnoreBegin + "\n" + strings.Join(managedIgnoreLines, "\n") + "\n" + gitIgnoreEnd
	start := strings.Index(existing, gitIgnoreBegin)
	end := strings.Index(existing, gitIgnoreEnd)
	endMarkerLen := len(gitIgnoreEnd)
	if start < 0 || end <= start {
		start = strings.Index(existing, legacyGitIgnoreBegin)
		end = strings.Index(existing, legacyGitIgnoreEnd)
		endMarkerLen = len(legacyGitIgnoreEnd)
	}

	var updated string
	if start >= 0 && end > start {
		end += endMarkerLen
		updated = existing[:start] + block + existing[end:]
	} else {
		trimmed := strings.TrimRight(existing, "\r\n")
		if trimmed == "" {
			updated = block + "\n"
		} else {
			updated = trimmed + "\n\n" + block + "\n"
		}
	}
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	if updated == existing {
		return nil
	}
	return os.WriteFile(p, []byte(updated), 0o644)
}

// CheckpointPush exports portable config, ensures .gitignore safety rules, commits
// changes if any, and pushes to origin when configured.
func CheckpointPush(ctx context.Context, cfg *config.HubConfig, hostID string) (CheckpointResult, error) {
	if cfg == nil || strings.TrimSpace(cfg.ProjectRoot) == "" {
		return CheckpointResult{}, errors.New("hub config with projectRoot is required")
	}
	root := cfg.ProjectRoot
	if err := EnsureGitIgnore(root); err != nil {
		return CheckpointResult{}, err
	}

	portablePath := filepath.Join(root, "config", "portable.json")
	if err := os.MkdirAll(filepath.Dir(portablePath), 0o755); err != nil {
		return CheckpointResult{}, err
	}
	bundle := config.ExportPortable(cfg, hostID)
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return CheckpointResult{}, err
	}
	raw = append(raw, '\n')
	if err := atomicWriteFile(portablePath, raw, 0o644, time.Time{}); err != nil {
		return CheckpointResult{}, err
	}

	branch := "main"
	repoURL := ""
	if cfg.Sync != nil && cfg.Sync.Git != nil {
		branch = cfg.Sync.Git.EffectiveBranch()
		repoURL = strings.TrimSpace(cfg.Sync.Git.RepoURL)
	}

	if err := ensureGitRepo(ctx, root, branch, repoURL); err != nil {
		return CheckpointResult{}, err
	}

	if _, err := runGit(ctx, root, "add", "-A"); err != nil {
		return CheckpointResult{}, err
	}
	statusOut, err := runGit(ctx, root, "status", "--porcelain")
	if err != nil {
		return CheckpointResult{}, err
	}

	res := CheckpointResult{Branch: branch}
	if strings.TrimSpace(statusOut) != "" {
		tag := sanitizeHostTag(hostID)
		msg := fmt.Sprintf("chore(sync): checkpoint from %s", tag)
		if _, err := runGit(ctx, root, "commit", "-m", msg); err != nil {
			return CheckpointResult{}, err
		}
		res.Committed = true
		res.Message = msg
		if sha, err := runGit(ctx, root, "rev-parse", "--short", "HEAD"); err == nil {
			res.CommitSHA = strings.TrimSpace(sha)
		}
	}

	if hasOriginRemote(ctx, root) {
		if _, err := runGit(ctx, root, "push", "-u", "origin", branch); err != nil {
			return res, err
		}
		res.Pushed = true
	}
	return res, nil
}

// CheckpointPull pulls the latest checkpoint from origin (if configured) and applies
// config/portable.json plus workspace glue.
func CheckpointPull(ctx context.Context, cfg *config.HubConfig) (PullResult, error) {
	if cfg == nil || strings.TrimSpace(cfg.ProjectRoot) == "" {
		return PullResult{}, errors.New("hub config with projectRoot is required")
	}
	root := cfg.ProjectRoot
	if err := EnsureGitIgnore(root); err != nil {
		return PullResult{}, err
	}

	branch := "main"
	repoURL := ""
	if cfg.Sync != nil && cfg.Sync.Git != nil {
		branch = cfg.Sync.Git.EffectiveBranch()
		repoURL = strings.TrimSpace(cfg.Sync.Git.RepoURL)
	}
	if err := ensureGitRepo(ctx, root, branch, repoURL); err != nil {
		return PullResult{}, err
	}

	res := PullResult{Branch: branch}
	if hasOriginRemote(ctx, root) {
		if _, headErr := runGit(ctx, root, "rev-parse", "--verify", "HEAD"); headErr != nil {
			if _, err := runGit(ctx, root, "fetch", "origin", branch); err != nil {
				return res, err
			}
			if _, err := runGit(ctx, root, "checkout", "-f", "-B", branch, "FETCH_HEAD"); err != nil {
				return res, err
			}
		} else {
			_, pullErr := runGit(ctx, root, "pull", "--rebase", "--autostash", "origin", branch)
			if resolveErr := resolveRebaseConflicts(ctx, root); resolveErr != nil {
				_, _ = runGit(ctx, root, "rebase", "--abort")
				if pullErr != nil {
					return res, fmt.Errorf("%w (auto-resolve: %v)", pullErr, resolveErr)
				}
				return res, resolveErr
			} else if pullErr != nil {
				// If pullErr failed for a non-conflict reason (no unmerged files were found), propagate it.
				if _, rebaseHeadErr := os.Stat(filepath.Join(root, ".git", "rebase-merge")); rebaseHeadErr == nil {
					_, _ = runGit(ctx, root, "rebase", "--abort")
					return res, pullErr
				}
			}
		}
		res.Pulled = true
	}

	portablePath := filepath.Join(root, "config", "portable.json")
	if raw, err := os.ReadFile(portablePath); err == nil {
		var bundle config.PortableBundle
		if err := json.Unmarshal(raw, &bundle); err != nil {
			return res, fmt.Errorf("parse portable.json: %w", err)
		}
		changed, added, updated, err := config.ApplyPortable(cfg, bundle)
		if err != nil {
			return res, err
		}
		res.ConfigChange = changed
		res.AddedBots = added
		res.UpdatedBots = updated
	}

	if err := RebuildWorkspaceGlue(cfg); err != nil {
		return res, err
	}
	return res, nil
}

func ensureGitRepo(ctx context.Context, root, branch, repoURL string) error {
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		if _, err := runGit(ctx, root, "init", "-b", branch); err != nil {
			// Fallback for older git without -b
			if _, err2 := runGit(ctx, root, "init"); err2 != nil {
				return err2
			}
			_, _ = runGit(ctx, root, "checkout", "-B", branch)
		}
	}
	// Ensure local committer identity exists so automated checkpoints never fail on fresh machines.
	if name, err := runGit(ctx, root, "config", "--get", "user.name"); err != nil || strings.TrimSpace(name) == "" {
		_, _ = runGit(ctx, root, "config", "user.name", "Pebble Sync")
	}
	if email, err := runGit(ctx, root, "config", "--get", "user.email"); err != nil || strings.TrimSpace(email) == "" {
		_, _ = runGit(ctx, root, "config", "user.email", "sync@pebble.local")
	}
	if repoURL != "" {
		if cur, err := runGit(ctx, root, "remote", "get-url", "origin"); err != nil {
			if _, err2 := runGit(ctx, root, "remote", "add", "origin", repoURL); err2 != nil {
				return err2
			}
		} else if strings.TrimSpace(cur) != repoURL {
			if _, err2 := runGit(ctx, root, "remote", "set-url", "origin", repoURL); err2 != nil {
				return err2
			}
		}
	}
	return nil
}

func hasOriginRemote(ctx context.Context, root string) bool {
	out, err := runGit(ctx, root, "remote", "get-url", "origin")
	return err == nil && strings.TrimSpace(out) != ""
}

func resolveRebaseConflicts(ctx context.Context, root string) error {
	for step := 0; step < 10; step++ {
		out, err := runGit(ctx, root, "diff", "--name-only", "--diff-filter=U")
		if err != nil {
			return err
		}
		var conflicted []string
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if f := strings.TrimSpace(line); f != "" {
				conflicted = append(conflicted, f)
			}
		}
		if len(conflicted) == 0 {
			return nil
		}
		for _, rel := range conflicted {
			ours, errOurs := runGitBytes(ctx, root, "show", ":2:"+rel)
			theirs, errTheirs := runGitBytes(ctx, root, "show", ":3:"+rel)
			if errOurs == nil {
				if abs, sErr := safeJoinAllowConfig(root, rel); sErr == nil {
					_ = atomicWriteFile(abs, ours, 0o644, time.Time{})
				}
			} else if errTheirs == nil {
				if abs, sErr := safeJoinAllowConfig(root, rel); sErr == nil {
					_ = atomicWriteFile(abs, theirs, 0o644, time.Time{})
				}
			}
			if errOurs == nil && errTheirs == nil && !bytes.Equal(ours, theirs) {
				conflictRel := conflictRelPath(rel, "checkpoint")
				if cAbs, sErr := safeJoinAllowConfig(root, conflictRel); sErr == nil {
					_ = atomicWriteFile(cAbs, theirs, 0o644, time.Time{})
				}
			}
			if _, addErr := runGit(ctx, root, "add", rel); addErr != nil {
				return addErr
			}
		}
		if _, err := os.Stat(filepath.Join(root, ".git", "rebase-merge")); os.IsNotExist(err) {
			if _, err2 := os.Stat(filepath.Join(root, ".git", "rebase-apply")); os.IsNotExist(err2) {
				// Resolved an autostash pop conflict outside rebase; unstage clean main file and drop stash.
				_, _ = runGit(ctx, root, "reset", "HEAD")
				_, _ = runGit(ctx, root, "stash", "drop")
				return nil
			}
		}
		cmd := exec.CommandContext(ctx, "git", "rebase", "--continue")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if contErr := cmd.Run(); contErr == nil {
			return nil
		}
	}
	return errors.New("exceeded max rebase conflict resolution steps")
}

func safeJoinAllowConfig(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absTarget := filepath.Join(absRoot, filepath.FromSlash(filepath.Clean(rel)))
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %q", rel)
	}
	return absTarget, nil
}

func runGitBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

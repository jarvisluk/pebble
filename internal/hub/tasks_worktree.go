package hub

import (
	"bot-hub/internal/state"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TaskDiffFile summarizes a single file changed in a BotTask workspace/checkpoint.
type TaskDiffFile struct {
	Path      string `json:"path"`
	Status    string `json:"status,omitempty"`
	Additions int    `json:"additions,omitempty"`
	Deletions int    `json:"deletions,omitempty"`
}

// TaskDiffResult describes the git diff and hidden checkpoint state for a BotTask.
type TaskDiffResult struct {
	TaskID        string         `json:"taskId"`
	Branch        string         `json:"branch,omitempty"`
	WorktreePath  string         `json:"worktreePath,omitempty"`
	CheckpointRef string         `json:"checkpointRef,omitempty"`
	Files         []TaskDiffFile `json:"files"`
	Patch         string         `json:"patch"`
}

// TaskRollbackResult describes the outcome of rolling back a BotTask's isolated changes.
type TaskRollbackResult struct {
	TaskID          string `json:"taskId"`
	RolledBack      bool   `json:"rolledBack"`
	RemovedWorktree bool   `json:"removedWorktree,omitempty"`
	Branch          string `json:"branch,omitempty"`
	WorktreePath    string `json:"worktreePath,omitempty"`
}

func (h *Hub) stateDir() string {
	if h.store != nil && h.store.DBPath() != "" && h.store.DBPath() != ":memory:" {
		return filepath.Dir(h.store.DBPath())
	}
	if h.cfg != nil && h.cfg.StateFile != "" {
		return filepath.Dir(h.cfg.StateFile)
	}
	if h.cfg != nil && h.cfg.MessagesDir != "" {
		return filepath.Dir(h.cfg.MessagesDir)
	}
	if h.cfg != nil && h.cfg.ProjectRoot != "" {
		return filepath.Join(h.cfg.ProjectRoot, "state")
	}
	return filepath.Join(os.TempDir(), "pebble-state")
}

func sanitizeTaskPathSegment(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), ".")
	if s == "" {
		return "task"
	}
	return s
}

func normalizeTaskIsolation(raw string) (string, error) {
	iso := strings.ToLower(strings.TrimSpace(raw))
	switch iso {
	case "", state.TaskIsolationCurrent, state.TaskIsolationWorktree, state.TaskIsolationTempDir:
		return iso, nil
	default:
		return "", fmt.Errorf("invalid isolation %q (expected current, worktree, or temp_dir)", raw)
	}
}

func evalCleanPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(abs)
}

func runGit(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var fullArgs []string
	if strings.TrimSpace(dir) != "" {
		fullArgs = append(fullArgs, "-C", dir)
	}
	fullArgs = append(fullArgs, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

func gitRepoTopLevel(ctx context.Context, dir string) (string, bool) {
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", false
	}
	top, err := runGit(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(top) == "" {
		return "", false
	}
	return evalCleanPath(top), true
}

func (h *Hub) prepareTaskIsolation(ctx context.Context, task *state.BotTask) error {
	if task == nil || isRemoteAgentTask(task.Kind, task.DeviceID) {
		return nil
	}
	seg := sanitizeTaskPathSegment(task.ID)
	switch task.Isolation {
	case state.TaskIsolationWorktree:
		baseDir := strings.TrimSpace(task.Workdir)
		if baseDir == "" {
			return errors.New("workdir is required for worktree isolation")
		}
		repoRoot, ok := gitRepoTopLevel(ctx, baseDir)
		if !ok {
			return fmt.Errorf("isolation %q requires workdir %q to be inside a git repository", state.TaskIsolationWorktree, baseDir)
		}
		if _, err := runGit(ctx, repoRoot, nil, "rev-parse", "--verify", "HEAD"); err != nil {
			return fmt.Errorf("git repository %q has no HEAD commit for worktree isolation: %w", repoRoot, err)
		}
		wtRoot := filepath.Join(h.stateDir(), "worktrees")
		if err := os.MkdirAll(wtRoot, 0o755); err != nil {
			return err
		}
		wtPath := filepath.Join(wtRoot, seg)
		branch := "pebble/task-" + seg
		if _, err := runGit(ctx, repoRoot, nil, "worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
			return err
		}
		task.WorktreePath = wtPath
		task.WorktreeBranch = branch

	case state.TaskIsolationTempDir:
		dir := filepath.Join(h.stateDir(), "task-dirs", seg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		task.WorktreePath = dir
	}
	return nil
}

func (h *Hub) effectiveTaskExecDir(task state.BotTask, botWorkspace string) string {
	if strings.TrimSpace(task.WorktreePath) != "" {
		if task.Isolation == state.TaskIsolationWorktree && strings.TrimSpace(task.Workdir) != "" {
			if repoRoot, ok := gitRepoTopLevel(context.Background(), task.Workdir); ok {
				if rel, err := filepath.Rel(repoRoot, evalCleanPath(task.Workdir)); err == nil && rel != "." && rel != "" && !strings.HasPrefix(rel, "..") {
					sub := filepath.Join(task.WorktreePath, rel)
					if info, err := os.Stat(sub); err == nil && info.IsDir() {
						return sub
					}
				}
			}
		}
		return task.WorktreePath
	}
	if strings.TrimSpace(task.Workdir) != "" {
		return task.Workdir
	}
	return botWorkspace
}

func (h *Hub) captureTaskCheckpoint(ctx context.Context, task *state.BotTask) error {
	if task == nil || isRemoteAgentTask(task.Kind, task.DeviceID) {
		return nil
	}
	if task.Isolation != state.TaskIsolationWorktree && task.Isolation != state.TaskIsolationCurrent {
		return nil
	}
	targetDir := strings.TrimSpace(task.WorktreePath)
	if targetDir == "" {
		targetDir = strings.TrimSpace(task.Workdir)
	}
	repoRoot, ok := gitRepoTopLevel(ctx, targetDir)
	if !ok {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "pebble-ckpt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	idxPath := filepath.Join(tmpDir, "index")
	gitEnv := []string{
		"GIT_INDEX_FILE=" + idxPath,
		"GIT_AUTHOR_NAME=Pebble",
		"GIT_AUTHOR_EMAIL=pebble@local",
		"GIT_COMMITTER_NAME=Pebble",
		"GIT_COMMITTER_EMAIL=pebble@local",
	}

	headSHA, headErr := runGit(ctx, repoRoot, nil, "rev-parse", "--verify", "HEAD")
	if headErr == nil && headSHA != "" {
		if _, err := runGit(ctx, repoRoot, gitEnv, "read-tree", "HEAD"); err != nil {
			return err
		}
	}
	if _, err := runGit(ctx, repoRoot, gitEnv, "add", "-A"); err != nil {
		return err
	}
	treeSHA, err := runGit(ctx, repoRoot, gitEnv, "write-tree")
	if err != nil {
		return err
	}

	commitArgs := []string{"commit-tree", treeSHA, "-m", "pebble checkpoint " + task.ID}
	if headErr == nil && headSHA != "" {
		commitArgs = append(commitArgs, "-p", headSHA)
	}
	commitSHA, err := runGit(ctx, repoRoot, gitEnv, commitArgs...)
	if err != nil {
		return err
	}

	refName := "refs/pebble/checkpoints/" + sanitizeTaskPathSegment(task.ID)
	if _, err := runGit(ctx, repoRoot, nil, "update-ref", refName, commitSHA); err != nil {
		return err
	}

	task.CheckpointRef = refName
	if h.store != nil {
		_ = h.store.UpdateBotTask(*task)
	}
	return nil
}

func (h *Hub) resolveTaskGitRepo(ctx context.Context, task state.BotTask) (string, error) {
	for _, cand := range []string{task.WorktreePath, task.Workdir} {
		if strings.TrimSpace(cand) == "" {
			continue
		}
		if root, ok := gitRepoTopLevel(ctx, cand); ok {
			return root, nil
		}
	}
	return "", errors.New("task does not have an accessible git workspace")
}

// GetTaskDiff computes the git diff files and patch for a BotTask against its base commit and hidden checkpoint.
func (h *Hub) GetTaskDiff(ctx context.Context, taskID string) (*TaskDiffResult, error) {
	if h.store == nil {
		return nil, errors.New("state store unavailable")
	}
	task, ok, err := h.store.GetBotTask(strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("task %q not found: %w", taskID, state.ErrTaskNotFound)
	}

	res := &TaskDiffResult{
		TaskID:        task.ID,
		Branch:        task.WorktreeBranch,
		WorktreePath:  task.WorktreePath,
		CheckpointRef: task.CheckpointRef,
		Files:         []TaskDiffFile{},
	}

	repoDir, err := h.resolveTaskGitRepo(ctx, task)
	if err != nil {
		return res, err
	}

	var baseRef, targetRef string
	if task.CheckpointRef != "" {
		if _, err := runGit(ctx, repoDir, nil, "rev-parse", "--verify", task.CheckpointRef); err == nil {
			targetRef = task.CheckpointRef
			if parent, perr := runGit(ctx, repoDir, nil, "rev-parse", "--verify", task.CheckpointRef+"^"); perr == nil && parent != "" {
				baseRef = parent
			}
		}
	}

	if targetRef == "" {
		// Fallback for active/uncheckpointed local git workspace: snapshot index in temp file.
		headSHA, herr := runGit(ctx, repoDir, nil, "rev-parse", "--verify", "HEAD")
		if herr != nil || headSHA == "" {
			return res, nil
		}
		baseRef = headSHA
		tmpDir, merr := os.MkdirTemp("", "pebble-diff-*")
		if merr != nil {
			return nil, merr
		}
		defer os.RemoveAll(tmpDir)
		gitEnv := []string{"GIT_INDEX_FILE=" + filepath.Join(tmpDir, "index")}
		_, _ = runGit(ctx, repoDir, gitEnv, "read-tree", "HEAD")
		if _, err := runGit(ctx, repoDir, gitEnv, "add", "-A"); err != nil {
			return nil, err
		}
		treeSHA, err := runGit(ctx, repoDir, gitEnv, "write-tree")
		if err != nil {
			return nil, err
		}
		targetRef = treeSHA
	} else if baseRef == "" {
		// Root commit checkpoint (no parent): diff against empty tree.
		baseRef = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	}

	statusOut, err := runGit(ctx, repoDir, nil, "diff", "--name-status", "--no-renames", baseRef, targetRef)
	if err != nil {
		return nil, err
	}
	numOut, err := runGit(ctx, repoDir, nil, "diff", "--numstat", "--no-renames", baseRef, targetRef)
	if err != nil {
		return nil, err
	}
	patchOut, err := runGit(ctx, repoDir, nil, "diff", "--no-color", baseRef, targetRef)
	if err != nil {
		return nil, err
	}

	statusByPath := make(map[string]string)
	for _, line := range strings.Split(statusOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			statusByPath[parts[1]] = parts[0]
		}
	}

	var files []TaskDiffFile
	for _, line := range strings.Split(numOut, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		addN, _ := strconv.Atoi(parts[0])
		delN, _ := strconv.Atoi(parts[1])
		p := parts[2]
		files = append(files, TaskDiffFile{
			Path:      p,
			Status:    statusByPath[p],
			Additions: addN,
			Deletions: delN,
		})
	}
	if files == nil {
		files = []TaskDiffFile{}
	}
	res.Files = files
	res.Patch = patchOut
	return res, nil
}

// RollbackTask restores the workspace changes or removes the task's isolated worktree and branch cleanly.
func (h *Hub) RollbackTask(ctx context.Context, taskID string, removeWorktree bool) (*TaskRollbackResult, error) {
	if h.store == nil {
		return nil, errors.New("state store unavailable")
	}
	task, ok, err := h.store.GetBotTask(strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("task %q not found: %w", taskID, state.ErrTaskNotFound)
	}
	if !state.IsTaskTerminalStatus(task.Status) {
		return nil, errors.New("cannot rollback active task; cancel it first")
	}

	res := &TaskRollbackResult{
		TaskID:       task.ID,
		RolledBack:   true,
		Branch:       task.WorktreeBranch,
		WorktreePath: task.WorktreePath,
	}

	switch {
	case task.Isolation == state.TaskIsolationWorktree || task.WorktreeBranch != "":
		var mainRepo string
		if task.Workdir != "" {
			mainRepo, _ = gitRepoTopLevel(ctx, task.Workdir)
		}
		if mainRepo == "" && task.WorktreePath != "" {
			mainRepo, _ = gitRepoTopLevel(ctx, task.WorktreePath)
		}
		if removeWorktree {
			if task.WorktreePath != "" {
				if mainRepo != "" {
					if _, err := runGit(ctx, mainRepo, nil, "worktree", "remove", "--force", task.WorktreePath); err != nil {
						_ = os.RemoveAll(task.WorktreePath)
						_, _ = runGit(ctx, mainRepo, nil, "worktree", "prune")
					}
				} else {
					_ = os.RemoveAll(task.WorktreePath)
				}
				res.RemovedWorktree = true
			}
			if mainRepo != "" && task.WorktreeBranch != "" {
				_, _ = runGit(ctx, mainRepo, nil, "branch", "-D", task.WorktreeBranch)
			}
			if mainRepo != "" && task.CheckpointRef != "" {
				_, _ = runGit(ctx, mainRepo, nil, "update-ref", "-d", task.CheckpointRef)
			}
			task.WorktreePath = ""
			task.WorktreeBranch = ""
			task.CheckpointRef = ""
		} else if task.WorktreePath != "" {
			if _, err := os.Stat(task.WorktreePath); err == nil {
				baseRef := "HEAD"
				if task.CheckpointRef != "" {
					if parent, perr := runGit(ctx, task.WorktreePath, nil, "rev-parse", "--verify", task.CheckpointRef+"^"); perr == nil && parent != "" {
						baseRef = parent
					}
				}
				if _, err := runGit(ctx, task.WorktreePath, nil, "reset", "--hard", baseRef); err != nil {
					return nil, err
				}
				if _, err := runGit(ctx, task.WorktreePath, nil, "clean", "-fd"); err != nil {
					return nil, err
				}
			}
			if mainRepo != "" && task.CheckpointRef != "" {
				_, _ = runGit(ctx, mainRepo, nil, "update-ref", "-d", task.CheckpointRef)
			}
			task.CheckpointRef = ""
		}

	case task.Isolation == state.TaskIsolationTempDir:
		if removeWorktree && task.WorktreePath != "" {
			if err := os.RemoveAll(task.WorktreePath); err != nil {
				return nil, err
			}
			res.RemovedWorktree = true
			task.WorktreePath = ""
		}

	default:
		if task.CheckpointRef != "" && task.Workdir != "" {
			if repoRoot, ok := gitRepoTopLevel(ctx, task.Workdir); ok {
				diffRes, derr := h.GetTaskDiff(ctx, task.ID)
				if derr == nil && strings.TrimSpace(diffRes.Patch) != "" {
					cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "apply", "-R", "-")
					cmd.Stdin = strings.NewReader(diffRes.Patch + "\n")
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil {
						return nil, fmt.Errorf("git apply -R: %s", strings.TrimSpace(stderr.String()))
					}
				}
				_, _ = runGit(ctx, repoRoot, nil, "update-ref", "-d", task.CheckpointRef)
				task.CheckpointRef = ""
			}
		}
	}

	_ = h.store.UpdateBotTask(task)
	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
		"status":          task.Status,
		"rolledBack":      true,
		"removedWorktree": res.RemovedWorktree,
		"ts":              time.Now().UTC().Format(time.RFC3339Nano),
	})
	h.broadcast(Event{
		Type:    "task_updated",
		BotID:   task.BotID,
		Payload: task,
	})
	return res, nil
}

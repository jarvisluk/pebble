package hub

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"bot-hub/internal/state"
)

var (
	ErrWorkspacePathBlocked = errors.New("workspace path is outside allowed sandbox or blocked")
	ErrWorkspaceIsDir       = errors.New("path is a directory")
	ErrWorkspaceNotFound    = errors.New("workspace path not found")
	ErrInvalidStageKind     = errors.New("invalid stage kind")
)

const (
	defaultWorkspaceFileMaxBytes = 256 * 1024
	hardWorkspaceFileMaxBytes    = 512 * 1024
	maxWorkspaceListEntries      = 1000
)

// WorkspaceFileEntry describes one file or directory inside a bot's workspace.
type WorkspaceFileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	IsSymlink bool   `json:"isSymlink,omitempty"`
	IsShared  bool   `json:"isShared,omitempty"`
	Size      int64  `json:"size"`
	ModTime   string `json:"modTime"`
	MimeType  string `json:"mimeType,omitempty"`
}

// WorkspaceListResult is returned by ListWorkspaceFiles.
type WorkspaceListResult struct {
	BotID     string               `json:"botId"`
	Path      string               `json:"path"`
	Parent    string               `json:"parent"`
	Entries   []WorkspaceFileEntry `json:"entries"`
	Truncated bool                 `json:"truncated,omitempty"`
}

// WorkspaceFileContent is returned by ReadWorkspaceFile.
type WorkspaceFileContent struct {
	BotID     string `json:"botId"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	ModTime   string `json:"modTime"`
	MimeType  string `json:"mimeType"`
	Kind      string `json:"kind"` // text | markdown | image | binary
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// StageOpenPayload describes a stage panel/tab open request pushed to clients.
type StageOpenPayload struct {
	ID          string         `json:"id,omitempty"`
	Kind        string         `json:"kind"` // file | files | desktop | task | browser | artifact | mcp-app
	Title       string         `json:"title,omitempty"`
	Subtitle    string         `json:"subtitle,omitempty"`
	BotID       string         `json:"botId,omitempty"`
	Path        string         `json:"path,omitempty"`
	URL         string         `json:"url,omitempty"`
	ResourceURI string         `json:"resourceUri,omitempty"`
	Target      string         `json:"target,omitempty"`
	TaskID      string         `json:"taskId,omitempty"`
	Closable    *bool          `json:"closable,omitempty"`
	AutoOpen    bool           `json:"autoOpen,omitempty"`
	Silent      bool           `json:"silent,omitempty"`
	CreatedAt   string         `json:"createdAt,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

func isSensitiveWorkspaceSegment(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	if lower == ".env" || strings.HasPrefix(lower, ".env.") {
		return true
	}
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".p12") {
		return true
	}
	switch lower {
	case "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa", ".netrc", ".npmrc", ".pypirc":
		return true
	}
	return false
}

func cleanWorkspaceRelPath(raw string) (string, error) {
	if strings.ContainsRune(raw, 0) || strings.ContainsRune(raw, '\\') {
		return "", ErrWorkspacePathBlocked
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "." || trimmed == "/" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "/") {
		return "", ErrWorkspacePathBlocked
	}
	parts := strings.Split(trimmed, "/")
	cleaned := make([]string, 0, len(parts))
	for _, seg := range parts {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." || isSensitiveWorkspaceSegment(seg) {
			return "", ErrWorkspacePathBlocked
		}
		cleaned = append(cleaned, seg)
	}
	return path.Join(cleaned...), nil
}

func evalSymlinksOrClean(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func isSharedRel(cleanRel string) bool {
	return cleanRel == "_shared" || strings.HasPrefix(cleanRel, "_shared/")
}

// ResolveWorkspacePath resolves a workspace-relative slash path safely inside the
// bot's workspace (or its top-level _shared symlink target).
func (h *Hub) ResolveWorkspacePath(botID, relPath string) (absPath, cleanRel string, isShared bool, err error) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return "", "", false, ErrBotNotFound
	}
	ws := strings.TrimSpace(bot.Workspace)
	if ws == "" {
		return "", "", false, ErrWorkspaceNotFound
	}
	cleanRel, err = cleanWorkspaceRelPath(relPath)
	if err != nil {
		return "", "", false, err
	}

	wsReal := evalSymlinksOrClean(ws)
	var sharedReal string
	if s, sErr := filepath.EvalSymlinks(filepath.Join(ws, "_shared")); sErr == nil {
		sharedReal = s
	}

	if cleanRel == "" {
		return wsReal, "", false, nil
	}

	candidate := filepath.Join(ws, filepath.FromSlash(cleanRel))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", false, ErrWorkspaceNotFound
		}
		return "", "", false, ErrWorkspacePathBlocked
	}

	if isSharedRel(cleanRel) {
		if sharedReal == "" || !isWithinRoot(sharedReal, resolved) {
			return "", "", false, ErrWorkspacePathBlocked
		}
		return resolved, cleanRel, true, nil
	}
	if !isWithinRoot(wsReal, resolved) {
		return "", "", false, ErrWorkspacePathBlocked
	}
	return resolved, cleanRel, false, nil
}

// ListWorkspaceFiles lists one directory level inside a bot's workspace.
func (h *Hub) ListWorkspaceFiles(botID, relPath string, showHidden bool) (*WorkspaceListResult, error) {
	absDir, cleanRel, dirShared, err := h.ResolveWorkspacePath(botID, relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrWorkspacePathBlocked
	}

	bot, _ := h.GetBot(botID)
	wsReal := evalSymlinksOrClean(bot.Workspace)
	var sharedReal string
	if s, sErr := filepath.EvalSymlinks(filepath.Join(bot.Workspace, "_shared")); sErr == nil {
		sharedReal = s
	}
	allowedRoot := wsReal
	if dirShared && sharedReal != "" {
		allowedRoot = sharedReal
	}

	dirEntries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}

	out := make([]WorkspaceFileEntry, 0, len(dirEntries))
	truncated := false

	for _, ent := range dirEntries {
		name := ent.Name()
		if isSensitiveWorkspaceSegment(name) {
			continue
		}
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !showHidden && skipMentionDirs[name] {
			continue
		}
		if showHidden && (name == ".git" || name == "node_modules" || name == ".uploads") {
			continue
		}

		entryRel := name
		if cleanRel != "" {
			entryRel = cleanRel + "/" + name
		}
		fullPath := filepath.Join(absDir, name)
		isSymlink := ent.Type()&fs.ModeSymlink != 0

		var stat os.FileInfo
		entryShared := dirShared
		if isSymlink {
			if cleanRel == "" && name == "_shared" && sharedReal != "" {
				s, sErr := os.Stat(fullPath)
				if sErr != nil || !s.IsDir() {
					continue
				}
				stat = s
				entryShared = true
			} else {
				resolved, rErr := filepath.EvalSymlinks(fullPath)
				if rErr != nil || !isWithinRoot(allowedRoot, resolved) {
					continue
				}
				s, sErr := os.Stat(resolved)
				if sErr != nil {
					continue
				}
				stat = s
			}
		} else {
			s, sErr := ent.Info()
			if sErr != nil {
				continue
			}
			stat = s
		}

		if len(out) >= maxWorkspaceListEntries {
			truncated = true
			break
		}

		var mimeType string
		if !stat.IsDir() {
			mimeType = detectWorkspaceMIME(fullPath, nil)
		}

		out = append(out, WorkspaceFileEntry{
			Name:      name,
			Path:      entryRel,
			IsDir:     stat.IsDir(),
			IsSymlink: isSymlink,
			IsShared:  entryShared,
			Size:      stat.Size(),
			ModTime:   stat.ModTime().UTC().Format(time.RFC3339),
			MimeType:  mimeType,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	parent := ""
	if cleanRel != "" {
		p := path.Dir(cleanRel)
		if p != "." {
			parent = p
		}
	}

	return &WorkspaceListResult{
		BotID:     botID,
		Path:      cleanRel,
		Parent:    parent,
		Entries:   out,
		Truncated: truncated,
	}, nil
}

func detectWorkspaceMIME(filePath string, head []byte) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".md", ".markdown", ".mdx":
		return "text/markdown; charset=utf-8"
	case ".ts", ".tsx", ".jsx", ".go", ".py", ".rs", ".swift", ".kt", ".java", ".sh", ".zsh", ".yaml", ".yml", ".toml", ".json", ".sql", ".css", ".html", ".xml", ".svg", ".txt", ".log", ".csv":
		if ext == ".svg" {
			return "image/svg+xml"
		}
		if ext == ".json" {
			return "application/json"
		}
		if ext == ".html" {
			return "text/html; charset=utf-8"
		}
		if ext == ".css" {
			return "text/css; charset=utf-8"
		}
		return "text/plain; charset=utf-8"
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}
	if len(head) > 0 {
		return http.DetectContentType(head)
	}
	return "application/octet-stream"
}

func isPreviewImageMIME(mimeType string) bool {
	base := strings.TrimSpace(strings.Split(strings.ToLower(mimeType), ";")[0])
	switch base {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func isMarkdownExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown" || ext == ".mdx"
}

// ReadWorkspaceFile reads a bounded preview of a workspace file.
func (h *Hub) ReadWorkspaceFile(botID, relPath string, maxBytes int) (*WorkspaceFileContent, error) {
	if strings.TrimSpace(relPath) == "" {
		return nil, ErrWorkspaceIsDir
	}
	absPath, cleanRel, _, err := h.ResolveWorkspacePath(botID, relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, ErrWorkspaceIsDir
	}

	if maxBytes <= 0 {
		maxBytes = defaultWorkspaceFileMaxBytes
	}
	if maxBytes > hardWorkspaceFileMaxBytes {
		maxBytes = hardWorkspaceFileMaxBytes
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	truncated := len(buf) > maxBytes
	if truncated {
		buf = buf[:maxBytes]
	}

	mimeType := detectWorkspaceMIME(absPath, buf)
	name := path.Base(cleanRel)
	res := &WorkspaceFileContent{
		BotID:     botID,
		Path:      cleanRel,
		Name:      name,
		Size:      info.Size(),
		ModTime:   info.ModTime().UTC().Format(time.RFC3339),
		MimeType:  mimeType,
		Truncated: truncated,
	}

	if isPreviewImageMIME(mimeType) {
		res.Kind = "image"
		return res, nil
	}

	// Treat UTF-8 text without NUL bytes as previewable text/markdown.
	if bytes.IndexByte(buf, 0) < 0 && utf8.Valid(buf) {
		if isMarkdownExt(name) {
			res.Kind = "markdown"
		} else {
			res.Kind = "text"
		}
		res.Content = string(buf)
		return res, nil
	}

	res.Kind = "binary"
	res.Truncated = false
	return res, nil
}

func (h *Hub) getBotLatestStage(botID string) *state.StageRef {
	if botID == "" {
		return nil
	}
	h.stageMu.RLock()
	cached := h.latestStages[botID]
	h.stageMu.RUnlock()
	if cached != nil {
		cp := *cached
		return &cp
	}
	if h.store != nil {
		if ref, ok := h.store.GetBotLatestStage(botID); ok && ref != nil {
			h.stageMu.Lock()
			h.latestStages[botID] = ref
			h.stageMu.Unlock()
			cp := *ref
			return &cp
		}
	}
	return nil
}

func (h *Hub) setBotLatestStage(botID string, ref state.StageRef) {
	if botID == "" {
		return
	}
	cp := ref
	h.stageMu.Lock()
	if h.latestStages == nil {
		h.latestStages = make(map[string]*state.StageRef)
	}
	h.latestStages[botID] = &cp
	h.stageMu.Unlock()
	if h.store != nil {
		_ = h.store.UpsertBotLatestStage(botID, ref)
	}
}

// OpenBotStage validates and broadcasts a stage_open event to connected clients,
// persists the bot's latest submitted stage tab, and posts a jump card in chat.
func (h *Hub) OpenBotStage(botID string, req StageOpenPayload) (*StageOpenPayload, error) {
	if _, ok := h.GetBot(botID); !ok {
		return nil, ErrBotNotFound
	}
	kind := strings.TrimSpace(req.Kind)
	switch kind {
	case "file", "files", "desktop", "task", "browser", "artifact", "mcp-app":
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidStageKind, kind)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	out := StageOpenPayload{
		ID:          strings.TrimSpace(req.ID),
		Kind:        kind,
		Title:       strings.TrimSpace(req.Title),
		Subtitle:    strings.TrimSpace(req.Subtitle),
		BotID:       botID,
		Path:        strings.TrimSpace(req.Path),
		URL:         strings.TrimSpace(req.URL),
		ResourceURI: strings.TrimSpace(req.ResourceURI),
		Target:      strings.TrimSpace(req.Target),
		TaskID:      strings.TrimSpace(req.TaskID),
		Closable:    req.Closable,
		AutoOpen:    req.AutoOpen,
		Silent:      req.Silent,
		CreatedAt:   now,
		Payload:     req.Payload,
	}

	if kind == "file" || (kind == "files" && out.Path != "") {
		if out.Path == "" {
			return nil, ErrWorkspaceNotFound
		}
		_, cleanRel, _, err := h.ResolveWorkspacePath(botID, out.Path)
		if err != nil {
			return nil, err
		}
		out.Path = cleanRel
		if out.Title == "" {
			out.Title = path.Base(cleanRel)
		}
		if out.Subtitle == "" {
			out.Subtitle = cleanRel
		}
	} else if kind == "desktop" {
		if out.Target == "" {
			out.Target = "local"
		}
		if out.Subtitle == "" && out.Target != "local" {
			out.Subtitle = out.Target
		}
	} else if kind == "task" {
		resolvedTaskID := out.TaskID
		if resolvedTaskID == "" && out.Payload != nil {
			if v, ok := out.Payload["taskId"].(string); ok {
				resolvedTaskID = strings.TrimSpace(v)
			}
		}
		if resolvedTaskID == "" && strings.HasPrefix(out.ID, "task:") {
			resolvedTaskID = strings.TrimSpace(strings.TrimPrefix(out.ID, "task:"))
		}
		if resolvedTaskID == "" {
			return nil, fmt.Errorf("%w: taskId is required", state.ErrTaskNotFound)
		}
		task, err := h.GetTask(resolvedTaskID)
		if err != nil {
			return nil, err
		}
		if task.BotID != botID {
			return nil, fmt.Errorf("%w: task %s does not belong to bot %s", state.ErrTaskNotFound, task.ID, botID)
		}
		out.TaskID = task.ID
		if out.Title == "" {
			title := strings.TrimSpace(task.Title)
			if title == "" {
				title = strings.TrimSpace(task.RoutineName)
			}
			if title == "" {
				line := strings.TrimSpace(strings.SplitN(task.Prompt, "\n", 2)[0])
				if len([]rune(line)) > 64 {
					line = string([]rune(line)[:61]) + "..."
				}
				title = line
			}
			if title == "" {
				title = task.ID
			}
			out.Title = title
		}
		if out.Subtitle == "" {
			var parts []string
			if task.Kind != "" {
				parts = append(parts, task.Kind)
			}
			if task.DeviceID != "" && task.DeviceID != "local" {
				parts = append(parts, task.DeviceID)
			}
			if task.Harness != "" {
				parts = append(parts, task.Harness)
			}
			if task.Status != "" {
				parts = append(parts, task.Status)
			}
			out.Subtitle = strings.Join(parts, " · ")
		}
	} else if kind == "browser" || kind == "artifact" || kind == "mcp-app" {
		if out.Subtitle == "" {
			if out.URL != "" {
				out.Subtitle = out.URL
			} else if out.ResourceURI != "" {
				out.Subtitle = out.ResourceURI
			}
		}
	}

	if out.ID == "" {
		switch {
		case kind == "file" || (kind == "files" && out.Path != ""):
			out.ID = "file:" + out.Path
		case kind == "files":
			out.ID = "files"
		case kind == "desktop":
			if out.Target != "" && out.Target != "local" {
				out.ID = "desktop:" + out.Target
			} else {
				out.ID = "desktop"
			}
		case kind == "task":
			out.ID = "task:" + out.TaskID
		default:
			out.ID = kind + ":" + botID + ":" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		}
	}

	ref := state.StageRef{
		ID:          out.ID,
		Kind:        out.Kind,
		Title:       out.Title,
		Subtitle:    out.Subtitle,
		Path:        out.Path,
		URL:         out.URL,
		ResourceURI: out.ResourceURI,
		Target:      out.Target,
		TaskID:      out.TaskID,
		Closable:    out.Closable,
		Payload:     out.Payload,
		CreatedAt:   out.CreatedAt,
	}
	h.setBotLatestStage(botID, ref)

	h.broadcast(Event{
		Type:    "stage_open",
		BotID:   botID,
		Payload: out,
	})

	if !req.Silent {
		fallbackText := out.Title
		if fallbackText == "" {
			fallbackText = out.Path
		}
		if fallbackText == "" {
			fallbackText = out.URL
		}
		if fallbackText == "" {
			fallbackText = out.Kind
		}
		h.appendMessage(state.ChatMessage{
			BotID:     botID,
			Role:      "stage",
			From:      "stage",
			Text:      fallbackText,
			Stage:     &ref,
			CreatedAt: out.CreatedAt,
		})
	}

	if st, err := h.statusOne(botID); err == nil {
		h.broadcast(Event{
			Type:    "bot_status",
			BotID:   botID,
			Payload: st,
		})
	}
	return &out, nil
}

package hub

import (
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type InboundAttachment struct {
	Media         state.MessageMedia `json:"media"`
	WorkspacePath string             `json:"workspacePath"`
}

type inboundAttEntry struct {
	media state.MessageMedia
	att   harness.Attachment
}

type OutboundImage struct {
	Path       string `json:"path,omitempty"`
	Alt        string `json:"alt,omitempty"`
	DataBase64 string `json:"dataBase64,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

type OutboundFile struct {
	Path       string `json:"path,omitempty"`
	Name       string `json:"name,omitempty"`
	DataBase64 string `json:"dataBase64,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

type OutboundAttachment struct {
	Path       string `json:"path,omitempty"`
	Name       string `json:"name,omitempty"`
	Alt        string `json:"alt,omitempty"`
	DataBase64 string `json:"dataBase64,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

const (
	maxOutboundImageBytes = 20 << 20 // 20 MiB
	maxOutboundFileBytes  = 50 << 20 // 50 MiB
)

var (
	validMediaIDRe = regexp.MustCompile(`^med_[0-9]+_[a-f0-9]+(\.[a-z0-9]{1,12})?$`)
	safeFileExtRe  = regexp.MustCompile(`^\.[a-z0-9]{1,12}$`)
)

func imageExtForMIME(mimeType string) (string, bool) {
	switch mimeType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func (h *Hub) SaveOutboundImage(botWorkspace string, img OutboundImage) (state.MessageMedia, error) {
	return h.SaveOutboundAttachment(botWorkspace, OutboundAttachment{
		Path:       img.Path,
		Alt:        img.Alt,
		DataBase64: img.DataBase64,
		Filename:   img.Filename,
	}, true)
}

func (h *Hub) SaveOutboundFile(botWorkspace string, f OutboundFile) (state.MessageMedia, error) {
	return h.SaveOutboundAttachment(botWorkspace, OutboundAttachment{
		Path:       f.Path,
		Name:       f.Name,
		DataBase64: f.DataBase64,
		Filename:   f.Filename,
	}, false)
}

func (h *Hub) SaveOutboundAttachment(botWorkspace string, att OutboundAttachment, requireImage bool) (state.MessageMedia, error) {
	maxBytes := int64(maxOutboundFileBytes)
	kindLabel := "file"
	if requireImage {
		maxBytes = maxOutboundImageBytes
		kindLabel = "image"
	}

	var raw []byte
	var name string

	if strings.TrimSpace(att.DataBase64) != "" {
		b64 := strings.TrimSpace(att.DataBase64)
		if idx := strings.Index(b64, ";base64,"); idx >= 0 {
			b64 = b64[idx+len(";base64,"):]
		}
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return state.MessageMedia{}, fmt.Errorf("invalid base64 %s: %w", kindLabel, err)
		}
		if int64(len(decoded)) > maxBytes {
			return state.MessageMedia{}, fmt.Errorf("%s exceeds max size (%d bytes)", kindLabel, maxBytes)
		}
		raw = decoded
		name = filepath.Base(strings.TrimSpace(att.Filename))
	} else {
		p := strings.TrimSpace(att.Path)
		if p == "" {
			return state.MessageMedia{}, fmt.Errorf("%s path or dataBase64 is required", kindLabel)
		}
		if !filepath.IsAbs(p) && botWorkspace != "" {
			p = filepath.Join(botWorkspace, p)
		}
		info, err := os.Stat(p)
		if err != nil {
			return state.MessageMedia{}, fmt.Errorf("stat %s %s: %w", kindLabel, p, err)
		}
		if info.IsDir() {
			return state.MessageMedia{}, fmt.Errorf("%s path %s is a directory", kindLabel, p)
		}
		if info.Size() > maxBytes {
			return state.MessageMedia{}, fmt.Errorf("%s %s exceeds max size (%d bytes)", kindLabel, p, maxBytes)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return state.MessageMedia{}, fmt.Errorf("read %s %s: %w", kindLabel, p, err)
		}
		raw = b
		name = filepath.Base(p)
	}

	if len(raw) == 0 {
		return state.MessageMedia{}, fmt.Errorf("%s is empty", kindLabel)
	}
	if customName := strings.TrimSpace(att.Name); customName != "" {
		name = filepath.Base(customName)
	}

	var mimeType, ext string
	if requireImage {
		mimeType = http.DetectContentType(raw)
		var ok bool
		ext, ok = imageExtForMIME(mimeType)
		if !ok {
			return state.MessageMedia{}, fmt.Errorf("unsupported image type %q", mimeType)
		}
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "image" + ext
		}
	} else {
		rawExt := strings.ToLower(filepath.Ext(name))
		if safeFileExtRe.MatchString(rawExt) {
			ext = rawExt
		}
		if ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
		if mimeType == "" {
			mimeType = http.DetectContentType(raw)
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "file" + ext
		}
	}

	if err := os.MkdirAll(h.mediaDir, 0o755); err != nil {
		return state.MessageMedia{}, fmt.Errorf("create media dir: %w", err)
	}
	rb := make([]byte, 4)
	_, _ = rand.Read(rb)
	id := fmt.Sprintf("med_%d_%s%s", time.Now().UTC().UnixMilli(), hex.EncodeToString(rb), ext)
	dst := filepath.Join(h.mediaDir, id)
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return state.MessageMedia{}, fmt.Errorf("write media file: %w", err)
	}

	return state.MessageMedia{
		ID:       id,
		Kind:     kindLabel,
		URL:      "/api/media/" + id,
		MimeType: mimeType,
		Name:     name,
		Alt:      strings.TrimSpace(att.Alt),
		Size:     int64(len(raw)),
	}, nil
}

func (h *Hub) ResolveMediaFile(mediaID string) (string, string, error) {
	id := strings.TrimSpace(mediaID)
	if !validMediaIDRe.MatchString(id) || strings.Contains(id, "..") || strings.ContainsAny(id, "/\\") {
		return "", "", fmt.Errorf("invalid media id")
	}
	p := filepath.Join(h.mediaDir, id)
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", "", os.ErrNotExist
	}
	ext := strings.ToLower(filepath.Ext(id))
	mimeType := ""
	if ext != "" {
		mimeType = mime.TypeByExtension(ext)
	}
	if mimeType == "" {
		if f, err := os.Open(p); err == nil {
			var buf [512]byte
			n, _ := f.Read(buf[:])
			_ = f.Close()
			if n > 0 {
				mimeType = http.DetectContentType(buf[:n])
			}
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return p, mimeType, nil
}

func sanitizeUploadFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "file"
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r == '/' || r == '\\' || r < 0x20:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return out
}

// SaveInboundAttachment stores a user-uploaded file in mediaDir and copies it
// into the bot's .uploads/ directory so the harness can read it directly.
func (h *Hub) SaveInboundAttachment(botID, filename string, raw []byte) (InboundAttachment, error) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return InboundAttachment{}, ErrBotNotFound
	}
	if len(raw) == 0 {
		return InboundAttachment{}, fmt.Errorf("attachment is empty")
	}
	detected := http.DetectContentType(raw)
	_, isImage := imageExtForMIME(detected)

	safeName := sanitizeUploadFilename(filename)
	b64 := base64.StdEncoding.EncodeToString(raw)
	media, err := h.SaveOutboundAttachment(bot.Workspace, OutboundAttachment{
		DataBase64: b64,
		Filename:   safeName,
		Name:       safeName,
	}, isImage)
	if err != nil {
		return InboundAttachment{}, err
	}

	wsRel := ""
	diskPath := filepath.Join(h.mediaDir, media.ID)
	if bot.Workspace != "" {
		uploadDir := filepath.Join(bot.Workspace, ".uploads")
		if err := os.MkdirAll(uploadDir, 0o755); err != nil {
			return InboundAttachment{}, fmt.Errorf("create uploads dir: %w", err)
		}
		uploadName := media.ID + "-" + safeName
		wsAbs := filepath.Join(uploadDir, uploadName)
		if err := os.WriteFile(wsAbs, raw, 0o644); err != nil {
			return InboundAttachment{}, fmt.Errorf("write workspace upload: %w", err)
		}
		diskPath = wsAbs
		wsRel = filepath.ToSlash(filepath.Join(".uploads", uploadName))
	}

	h.inboundAttsMu.Lock()
	if h.inboundAtts == nil {
		h.inboundAtts = make(map[string]inboundAttEntry)
	}
	h.inboundAtts[media.ID] = inboundAttEntry{
		media: media,
		att: harness.Attachment{
			ID:            media.ID,
			Kind:          media.Kind,
			Name:          media.Name,
			MimeType:      media.MimeType,
			Path:          diskPath,
			WorkspacePath: wsRel,
		},
	}
	h.inboundAttsMu.Unlock()

	return InboundAttachment{
		Media:         media,
		WorkspacePath: wsRel,
	}, nil
}

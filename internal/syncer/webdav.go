package syncer

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// NewWebDAVHandler returns an HTTP handler exposing workspacesDir over minimal WebDAV
// (OPTIONS, PROPFIND, GET, HEAD, and PUT/MKCOL/DELETE when readOnly is false).
func NewWebDAVHandler(workspacesDir string, readOnly bool) http.Handler {
	absRoot, _ := filepath.Abs(workspacesDir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if absRoot == "" {
			http.Error(w, "webdav root unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("DAV", "1")
			w.Header().Set("MS-Author-Via", "DAV")
			if readOnly {
				w.Header().Set("Allow", "OPTIONS, PROPFIND, GET, HEAD")
			} else {
				w.Header().Set("Allow", "OPTIONS, PROPFIND, GET, HEAD, PUT, MKCOL, DELETE")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		cleanRel := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		relTrim := strings.TrimPrefix(cleanRel, "/")
		if relTrim != "" && !isWebDAVAllowedRel(relTrim) {
			http.Error(w, "forbidden path", http.StatusForbidden)
			return
		}
		absTarget := filepath.Join(absRoot, filepath.FromSlash(relTrim))
		if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
			http.Error(w, "forbidden path", http.StatusForbidden)
			return
		}

		switch r.Method {
		case http.MethodGet, http.MethodHead:
			fi, err := os.Stat(absTarget)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if fi.IsDir() {
				http.Error(w, "directory listing requires PROPFIND", http.StatusMethodNotAllowed)
				return
			}
			http.ServeFile(w, r, absTarget)

		case "PROPFIND":
			servePropfind(w, r, absRoot, absTarget, cleanRel)

		case http.MethodPut:
			if readOnly {
				http.Error(w, "webdav is read-only", http.StatusForbidden)
				return
			}
			if relTrim == "" {
				http.Error(w, "cannot overwrite root", http.StatusBadRequest)
				return
			}
			if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, MaxSyncFileSize+1))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(data) > MaxSyncFileSize {
				http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			_, existed := os.Stat(absTarget)
			if err := atomicWriteFile(absTarget, data, 0o644, time.Time{}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if os.IsNotExist(existed) {
				w.WriteHeader(http.StatusCreated)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}

		case "MKCOL":
			if readOnly {
				http.Error(w, "webdav is read-only", http.StatusForbidden)
				return
			}
			if err := os.MkdirAll(absTarget, 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)

		case http.MethodDelete:
			if readOnly {
				http.Error(w, "webdav is read-only", http.StatusForbidden)
				return
			}
			if relTrim == "" {
				http.Error(w, "cannot delete root", http.StatusForbidden)
				return
			}
			if err := os.RemoveAll(absTarget); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func isWebDAVAllowedRel(rel string) bool {
	if rel == "" {
		return true
	}
	return ShouldSyncPath("workspaces/" + rel)
}

func servePropfind(w http.ResponseWriter, r *http.Request, absRoot, absTarget, cleanRel string) {
	fi, err := os.Stat(absTarget)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	depth := strings.TrimSpace(r.Header.Get("Depth"))
	if depth == "" {
		depth = "1"
	}

	type davProp struct {
		DisplayName   string `xml:"D:displayname,omitempty"`
		ContentLength int64  `xml:"D:getcontentlength,omitempty"`
		LastModified  string `xml:"D:getlastmodified,omitempty"`
		IsCollection  *struct{} `xml:"D:resourcetype>D:collection,omitempty"`
		ResourceType  string `xml:"-"`
	}
	type davPropStat struct {
		Prop   davProp `xml:"D:prop"`
		Status string  `xml:"D:status"`
	}
	type davResponse struct {
		Href     string      `xml:"D:href"`
		PropStat davPropStat `xml:"D:propstat"`
	}
	type davMultistatus struct {
		XMLName   xml.Name      `xml:"D:multistatus"`
		XmlnsD    string        `xml:"xmlns:D,attr"`
		Responses []davResponse `xml:"D:response"`
	}

	buildResp := func(href string, info os.FileInfo) davResponse {
		p := davProp{
			DisplayName:  info.Name(),
			LastModified: info.ModTime().UTC().Format(http.TimeFormat),
		}
		if info.IsDir() {
			p.IsCollection = &struct{}{}
			if !strings.HasSuffix(href, "/") {
				href += "/"
			}
		} else {
			p.ContentLength = info.Size()
		}
		return davResponse{
			Href: href,
			PropStat: davPropStat{
				Prop:   p,
				Status: "HTTP/1.1 200 OK",
			},
		}
	}

	baseHref := escapeWebDAVPath(cleanRel)
	if baseHref == "" {
		baseHref = "/"
	}
	resps := []davResponse{buildResp(baseHref, fi)}

	if fi.IsDir() && depth != "0" {
		entries, _ := os.ReadDir(absTarget)
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			childRel := strings.TrimPrefix(path.Join(cleanRel, entry.Name()), "/")
			if !isWebDAVAllowedRel(childRel) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			childHref := escapeWebDAVPath(path.Join(cleanRel, entry.Name()))
			resps = append(resps, buildResp(childHref, info))
		}
	}

	ms := davMultistatus{
		XmlnsD:    "DAV:",
		Responses: resps,
	}
	raw, err := xml.MarshalIndent(ms, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(raw)
}

func escapeWebDAVPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	res := strings.Join(parts, "/")
	if !strings.HasPrefix(res, "/") {
		res = "/" + res
	}
	return res
}

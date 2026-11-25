package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
)

func (s *HTTPServer) nodes() *node.Registry {
	if s.hub != nil && s.hub.Nodes() != nil {
		return s.hub.Nodes()
	}
	s.nodesOnce.Do(func() {
		s.fallbackNodes = node.NewRegistry(nil)
	})
	return s.fallbackNodes
}

func (s *HTTPServer) handleNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"nodes": s.nodes().List()})
}

func (s *HTTPServer) handleLocalNodeStub(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"available": false})
}

func (s *HTTPServer) handleNodePoll(w http.ResponseWriter, r *http.Request) {
	var req node.PollRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	cmd, err := s.nodes().Poll(r.Context(), req, 25*time.Second)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if cmd == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, cmd)
}

func (s *HTTPServer) handleNodeResult(w http.ResponseWriter, r *http.Request) {
	var res node.Result
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&res); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	ok := s.nodes().Deliver(res)
	writeJSON(w, http.StatusOK, map[string]any{"delivered": ok})
}

func (s *HTTPServer) handleNodeDisconnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	s.nodes().Disconnect(req.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) resolveTargetNodeInfo(nodeID string) (node.NodeInfo, bool) {
	if id := strings.TrimSpace(nodeID); id != "" {
		return s.nodes().Get(id)
	}
	var sole node.NodeInfo
	count := 0
	for _, n := range s.nodes().List() {
		if n.Connected {
			sole = n
			count++
			if count > 1 {
				return node.NodeInfo{}, false
			}
		}
	}
	if count == 1 {
		return sole, true
	}
	return node.NodeInfo{}, false
}

func (s *HTTPServer) handleNodeCall(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if nodeID == "_default" {
		nodeID = ""
	}
	var req struct {
		Action     string          `json:"action"`
		Args       json.RawMessage `json:"args"`
		TimeoutSec int             `json:"timeoutSec"`
		BotID      string          `json:"botId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	callArgs := req.Args
	if req.BotID != "" && s.hub != nil {
		session, isolated, boundNodeID, boundSlot := s.hub.GetBotBrowserInfo(req.BotID)
		if nodeID == "" && boundNodeID != "" {
			nodeID = boundNodeID
		}
		targetNode, hasTargetNode := s.resolveTargetNodeInfo(nodeID)
		if boundSlot != "" && (nodeID == "" || nodeID == boundNodeID) && hasTargetNode && !targetNode.HasCapability(protocol.CapNodeVirtualSlots) {
			writeNodeUpgradeErr(
				w,
				http.StatusUnprocessableEntity,
				fmt.Sprintf("remote node %q is outdated and does not support dedicated virtual screens; upgrade the node or bind its main desktop in Bot Details", targetNode.ID),
				protocol.CapNodeVirtualSlots,
			)
			return
		}
		if req.Action == "exec" {
			var m map[string]any
			if len(callArgs) == 0 || json.Unmarshal(callArgs, &m) == nil {
				if m == nil {
					m = make(map[string]any)
				}
				if !hasTargetNode || targetNode.HasCapability(protocol.CapNodeExecEnv) {
					envMap, _ := m["env"].(map[string]any)
					if envMap == nil {
						envMap = make(map[string]any)
					}
					if _, hasPebble := envMap["PEBBLE_BOT_ID"]; !hasPebble {
						if _, hasLegacy := envMap["BOT_HUB_BOT_ID"]; !hasLegacy {
							envMap["PEBBLE_BOT_ID"] = req.BotID
						}
					}
					if _, ok := envMap["AGENT_BROWSER_SESSION"]; !ok && session != "" {
						envMap["AGENT_BROWSER_SESSION"] = session
					}
					m["env"] = envMap
				}
				if isolated && (!hasTargetNode || targetNode.HasCapability(protocol.CapNodeBrowserProfile)) {
					if _, ok := m["browserProfileBotId"]; !ok {
						m["browserProfileBotId"] = req.BotID
					}
				}
				if boundSlot != "" && (nodeID == "" || nodeID == boundNodeID) && (!hasTargetNode || targetNode.HasCapability(protocol.CapNodeVirtualSlots)) {
					if _, ok := m["desktopSlot"]; !ok {
						m["desktopSlot"] = boundSlot
					}
				}
				if b, merr := json.Marshal(m); merr == nil {
					callArgs = b
				}
			}
		}
	}
	res, resolvedID, err := s.nodes().Call(r.Context(), nodeID, req.Action, callArgs, timeout)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if res != nil && res.OK && res.DataBase64 != "" {
		if path, saveErr := s.saveNodeBinary(req.BotID, resolvedID, res); saveErr == nil {
			res.SavedPath = path
			res.DataBase64 = ""
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) saveNodeBinary(botID, nodeID string, res *node.Result) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(res.DataBase64)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "bot-hub-remote")
	if s.hub != nil {
		if ws := s.hub.BotWorkspace(botID); ws != "" {
			dir = filepath.Join(ws, "_remote")
		} else if shared := s.hub.Config().SharedDir; shared != "" {
			dir = filepath.Join(shared, "remote")
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := ".jpg"
	if res.MimeType == "image/png" {
		ext = ".png"
	}
	name := fmt.Sprintf("%s-%d%s", filepath.Base(nodeID), time.Now().UnixMilli(), ext)
	fullPath := filepath.Join(dir, name)
	if err := os.WriteFile(fullPath, raw, 0o600); err != nil {
		return "", err
	}
	return fullPath, nil
}

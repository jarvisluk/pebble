package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/tunnel"
)

type tunnelResp struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
	Error string `json:"error,omitempty"`
}

var (
	tunnelMu sync.Mutex
	active   *tunnelInstance
)

type tunnelInstance struct {
	cancel      context.CancelFunc
	done        chan struct{}
	url         string
	token       string
	isTailscale bool
}

func startTunnelJSON(rawLink, explicitToken string) string {
	url, tok, err := startTunnelInternal(rawLink, explicitToken)
	if err != nil {
		b, _ := json.Marshal(tunnelResp{Error: err.Error()})
		return string(b)
	}
	b, _ := json.Marshal(tunnelResp{URL: url, Token: tok})
	return string(b)
}

func startTunnelInternal(rawLink, explicitToken string) (string, string, error) {
	cleanLink, fragTok := tunnel.ExtractTokenFromLink(rawLink)
	isTailscale := strings.HasPrefix(strings.ToLower(cleanLink), "tailscale://")
	var tsTarget string
	if isTailscale {
		var err error
		tsTarget, err = tunnel.ParseTailscaleLink(cleanLink)
		if err != nil {
			return "", "", err
		}
	} else if _, _, err := tunnel.ParseLink(cleanLink); err != nil {
		return "", "", err
	}
	tok := strings.TrimSpace(explicitToken)
	if tok == "" {
		tok = fragTok
	}

	stopTunnelInternal()

	ctx, cancel := context.WithCancel(context.Background())
	var (
		dial        func(context.Context) (net.Conn, error)
		closeClient func() error
	)
	if isTailscale {
		waitCtx, waitCancel := context.WithTimeout(ctx, 25*time.Second)
		_, err := dialTailscaleCheck(waitCtx)
		waitCancel()
		if err != nil {
			cancel()
			return "", "", err
		}
		dial = func(dctx context.Context) (net.Conn, error) {
			return dialTailscale(dctx, tsTarget)
		}
		closeClient = func() error { return nil }
	} else {
		var err error
		dial, closeClient, err = tunnel.Connect(ctx, cleanLink)
		if err != nil {
			cancel()
			return "", "", err
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		_ = closeClient()
		return "", "", err
	}

	localURL := "http://" + ln.Addr().String()
	tr := &http.Transport{
		DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) {
			return dial(dctx)
		},
	}

	rp := &httputil.ReverseProxy{
		Transport:     tr,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "127.0.0.1:3789"
			pr.Out.Host = "127.0.0.1:3789"
			pr.Out.Header.Del("Origin")
			if tok != "" && pr.Out.Header.Get("Authorization") == "" {
				pr.Out.Header.Set("Authorization", "Bearer "+tok)
			}
		},
	}

	srv := &http.Server{
		Handler:           rp,
		ReadHeaderTimeout: 10 * time.Second,
	}

	done := make(chan struct{})
	inst := &tunnelInstance{
		cancel:      cancel,
		done:        done,
		url:         localURL,
		token:       tok,
		isTailscale: isTailscale,
	}

	tunnelMu.Lock()
	active = inst
	tunnelMu.Unlock()

	go func() {
		defer close(done)
		defer func() {
			tr.CloseIdleConnections()
			_ = closeClient()
		}()
		<-ctx.Done()
		sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer scancel()
		_ = srv.Shutdown(sctx)
	}()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[libbothub] proxy serve: %v", err)
		}
	}()

	return localURL, tok, nil
}

func stopTunnelInternal() {
	tunnelMu.Lock()
	inst := active
	active = nil
	tunnelMu.Unlock()
	if inst == nil {
		return
	}
	inst.cancel()
	select {
	case <-inst.done:
	case <-time.After(3 * time.Second):
	}
}

func stopTailscaleTunnelIfActive() {
	tunnelMu.Lock()
	inst := active
	isTS := inst != nil && inst.isTailscale
	tunnelMu.Unlock()
	if isTS {
		stopTunnelInternal()
	}
}

func dialTailscaleCheck(ctx context.Context) (*tunnel.TailscaleNode, error) {
	tsMu.Lock()
	node := tsNode
	tsMu.Unlock()
	if node == nil {
		return nil, errors.New("tailscale is not bound")
	}
	if err := node.WaitRunning(ctx); err != nil {
		return nil, err
	}
	return node, nil
}

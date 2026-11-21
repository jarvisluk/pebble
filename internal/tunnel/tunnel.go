// Package tunnel exposes bot-hub over Tailcat (github.com/tailscale/tailcat):
// a WireGuard pipe bootstrapped through a DERP relay, needing no Tailscale
// account, tailnet login or system VPN on either end. The link carries the
// server's keys, so it is a secret; HTTP still requires http.apiToken.
package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/tailcat"
)

// Port is the port bot-hub announces on its Tailcat address.
const Port = 3789

// Listen starts a Tailcat server whose identity lives in identityFile (0600),
// so the link survives restarts. derpMapURL is optional (self-hosted DERP).
func Listen(ctx context.Context, identityFile, derpMapURL string) (net.Listener, string, func(), error) {
	id, err := loadIdentity(identityFile)
	if err != nil {
		return nil, "", nil, err
	}
	srv := &tailcat.Server{Key: id.Private, PresharedKey: id.Public.PresharedKey, DERPMapURL: derpMapURL, Logf: logf}
	if len(id.Public.Region) > 0 && derpMapURL == "" {
		srv.Region = id.Public.Region[0] // keep last run's relay: same link
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	ln, err := srv.Listen(ctx, "tcp", fmt.Sprintf(":%d", Port))
	if err != nil {
		srv.Close()
		return nil, "", nil, fmt.Errorf("tailcat: %w", err)
	}
	addr, err := srv.TailcatAddr().Resolve(ctx)
	if err == nil {
		var info tailcat.ConnInfo
		if info, err = tailcat.ParseAddr(addr); err == nil {
			id.Public = info
			err = saveIdentity(identityFile, id)
		}
	}
	if err != nil {
		ln.Close()
		srv.Close()
		return nil, "", nil, fmt.Errorf("tailcat: %w", err)
	}
	link := fmt.Sprintf("tailcat://connect/%s?port=%d", addr, Port)
	return ln, link, func() { ln.Close(); srv.Close() }, nil
}

// Connect validates link, establishes a Tailcat client and waits for the
// initial ping to succeed, returning a dialer for the target port and a close
// function.
func Connect(ctx context.Context, link string) (dial func(context.Context) (net.Conn, error), closeFn func() error, err error) {
	addr, port, err := ParseLink(link)
	if err != nil {
		return nil, nil, err
	}
	client := &tailcat.Client{Server: addr, Logf: logf}

	// Ping gives up after 10s whatever ctx says; the first DERP connect can take longer.
	for deadline := time.Now().Add(60 * time.Second); ; {
		if _, err = client.Ping(ctx); err == nil || ctx.Err() != nil || time.Now().After(deadline) {
			break
		}
	}
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("tailcat: server unreachable: %w", err)
	}
	return func(dctx context.Context) (net.Conn, error) {
		return client.DialTCPPort(dctx, port)
	}, client.Close, nil
}

// Forward serves localAddr, piping each connection to the bot-hub behind link,
// until ctx is done. ready is called once the server answered a ping.
func Forward(ctx context.Context, link, localAddr string, ready func(localURL string)) error {
	if _, _, err := ParseLink(link); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", localAddr)
	if err != nil {
		return err
	}
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()

	dial, closeClient, err := Connect(ctx, link)
	if err != nil {
		return err
	}
	defer closeClient()

	if ready != nil {
		ready("http://" + ln.Addr().String())
	}
	return ServeForward(ctx, ln, dial)
}

// ServeForward accepts connections on ln and proxies each one via dial until ctx is done.
func ServeForward(ctx context.Context, ln net.Listener, dial func(context.Context) (net.Conn, error)) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return ctx.Err()
		}
		go func() {
			dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			remote, err := dial(dctx)
			if err != nil {
				c.Close()
				return
			}
			tailcat.ProxyConns(c, remote)
		}()
	}
}

// ExtractTokenFromLink splits an optional `#token=<val>` fragment from a
// tailcat:// pairing link, returning the clean link and extracted token.
func ExtractTokenFromLink(raw string) (cleanLink, token string) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return raw, ""
	}
	if frag := u.EscapedFragment(); frag != "" {
		if q, qerr := url.ParseQuery(frag); qerr == nil {
			token = strings.TrimSpace(q.Get("token"))
		}
		u.Fragment = ""
		u.RawFragment = ""
	}
	return u.String(), token
}

// ParseLink validates a tailcat://connect/<addr>?port=N link without network I/O.
func ParseLink(link string) (tailcat.Addr, uint16, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "tailcat" || u.Host != "connect" {
		return "", 0, errors.New("invalid tailcat link")
	}
	port, err := strconv.ParseUint(u.Query().Get("port"), 10, 16)
	if err != nil || port == 0 {
		return "", 0, errors.New("invalid tailcat link port")
	}
	addr := tailcat.Addr(u.Path[min(1, len(u.Path)):])
	if _, err := tailcat.ParseAddr(addr); err != nil {
		return "", 0, errors.New("invalid tailcat address")
	}
	return addr, uint16(port), nil
}

func loadIdentity(path string) (*tailcat.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return tailcat.NewPrivateKey(), nil
	}
	if err != nil {
		return nil, err
	}
	var id tailcat.PrivateKey
	if json.Unmarshal(raw, &id) != nil || id.Private.IsZero() {
		return nil, fmt.Errorf("invalid tailcat identity %s (delete it to get a new link)", path)
	}
	return &id, nil
}

func saveIdentity(path string, id *tailcat.PrivateKey) error {
	raw, err := json.Marshal(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", raw, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func logf(format string, args ...any) {
	if os.Getenv("BOT_HUB_TAILCAT_DEBUG") == "1" {
		log.Printf("[tailcat] "+format, args...)
	}
}

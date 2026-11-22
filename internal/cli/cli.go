// Package cli holds bot-hub's commands, shared by the CLI and the desktop app.
package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bot-hub/internal/channel"
	"bot-hub/internal/config"
	"bot-hub/internal/desktop"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/mcp"
	"bot-hub/internal/node"
	"bot-hub/internal/scheduler"
	"bot-hub/internal/state"
	"bot-hub/internal/tunnel"
)

// Main runs a bot-hub command; args excludes the program name.
func Main(args []string) {
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	sub := args[0]
	if sub == "mcp" {
		runMCP(args[1:])
		return
	}
	if sub == "connect" {
		runConnect(args[1:])
		return
	}
	switch sub {
	case "bot", "session", "routine", "node", "desktop":
		cwd, _ := os.Getwd()
		os.Exit(runCommand(context.Background(), args, cwd, os.Stdout, os.Stderr))
	}

	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	cfgFlag := fs.String("config", "", "path to bots.json config file")
	var autoDesktop *bool
	if sub == "serve" {
		autoDesktop = fs.Bool("auto-desktop", false, "auto install headless Linux virtual desktop dependencies if missing")
	}
	_ = fs.Parse(args[1:])
	rest := fs.Args()

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}

	h, cfgPath, err := load(cwd, *cfgFlag)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	switch sub {
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if (autoDesktop != nil && *autoDesktop) || h.Config().Desktop.AutoInstall {
			go maybeAutoSetupDesktop(ctx, h.LocalDesktop())
		}
		if err := runServe(ctx, h, cfgPath); err != nil {
			log.Fatal(err)
		}
	case "chat":
		botID := h.Config().Bots[0].ID
		if len(rest) > 0 {
			botID = rest[0]
		}
		runChat(h, botID)
	case "status":
		runStatus(h, cfgPath)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage:
  bot-hub serve  [-config path] [-auto-desktop]                   Start Web UI, HTTP API, and scheduler
  bot-hub chat   [-config path] [bot]                             Interactive terminal chat with a bot
  bot-hub status [-config path]                                   Show configured bots and persisted sessions
  bot-hub mcp     [--hub-url url] [--bot-id id] [--config path]   Run stdio MCP server exposing core runtime primitives
  bot-hub connect [-listen addr] [-node] <tailcat-link>           Reach a remote bot-hub over Tailcat (optionally register as a node)
  bot-hub bot     <list|get|create|update|delete|message> [flags] Manage bots or send messages via running Hub API
  bot-hub session <list|recall|rotate> [flags]                    Inspect, recall, or rotate bot sessions
  bot-hub routine <list|upsert|delete|run> [flags]                Manage and trigger scheduled routines
  bot-hub node    <list|exec|file> [flags]                        List or invoke remote computer nodes
  bot-hub desktop <task|takeover|setup|power> [flags]             Delegate desktop GUI tasks, setup virtual desktop, or control power`)
}

func runConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:3790", "local address to serve the remote bot-hub on")
	asNode := fs.Bool("node", false, "also expose this computer as a remote device node")
	token := fs.String("token", "", "hub apiToken for node registration (or BOT_HUB_API_TOKEN)")
	nodeID := fs.String("node-id", "", "node ID (defaults to hostname)")
	perms := fs.String("perms", "files,exec,desktop", "comma-separated node permissions: files,exec,desktop")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage()
		os.Exit(1)
	}
	link, linkTok := tunnel.ExtractTokenFromLink(fs.Arg(0))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("[tailcat] connecting...")
	if !*asNode {
		err := tunnel.Forward(ctx, link, *listen, func(u string) { log.Printf("[tailcat] connected: open %s", u) })
		if err != nil && ctx.Err() == nil {
			log.Fatalf("%v", err)
		}
		return
	}

	if _, _, err := tunnel.ParseLink(link); err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()

	dial, closeClient, err := tunnel.Connect(ctx, link)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer closeClient()

	tok := strings.TrimSpace(*token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("BOT_HUB_API_TOKEN"))
	}
	if tok == "" {
		tok = linkTok
	}
	tr := &http.Transport{
		DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) { return dial(dctx) },
	}
	defer tr.CloseIdleConnections()

	agent := node.NewAgent(node.AgentOptions{
		NodeID:      *nodeID,
		BaseURL:     "http://" + ln.Addr().String(),
		Token:       tok,
		Permissions: node.ValidPerms(strings.Split(*perms, ",")),
		Enabled:     true,
		HTTPClient:  &http.Client{Transport: tr},
		OnState:     nil,
	})
	defer agent.Close()
	st := agent.Status()
	log.Printf("[tailcat] connected: open http://%s (node=%s perms=%v)", ln.Addr().String(), st.NodeID, st.Permissions)
	if err := tunnel.ServeForward(ctx, ln, dial); err != nil && ctx.Err() == nil {
		log.Fatalf("%v", err)
	}
}

func runMCP(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	hubURL := fs.String("hub-url", "http://127.0.0.1:3789", "running bot-hub HTTP base URL")
	botID := fs.String("bot-id", "", "caller bot ID")
	cfgPath := fs.String("config", "", "path to bots.json config file")
	_ = fs.Parse(args)

	srv := mcp.NewServerWithConfig(*hubURL, *botID, *cfgPath)
	if err := srv.ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
		os.Exit(1)
	}
}

func load(root, cfgFlag string) (*hub.Hub, string, error) {
	if cfgFlag == "" {
		if _, err := config.ResolvePath(root, ""); err != nil {
			if path, created, cerr := config.EnsureStarterConfig(root, harness.PreferredInstalledHarness()); cerr == nil && created {
				log.Printf("[hub] initialized starter config at %s", path)
			}
		}
	}
	cfg, cfgPath, err := config.Load(root, cfgFlag)
	if err != nil {
		return nil, "", fmt.Errorf("load config: %w", err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		return nil, "", fmt.Errorf("load state: %w", err)
	}
	msgStore, err := state.NewMessageStore(cfg.MessagesDir)
	if err != nil {
		return nil, "", fmt.Errorf("load message store: %w", err)
	}
	return hub.New(cfg, store, msgStore, harness.NewDefaultRegistry(cfg)), cfgPath, nil
}

// Serve runs the hub rooted at root (config/, state/, workspaces/) until ctx is done.
func Serve(ctx context.Context, root string) error {
	h, cfgPath, err := load(root, "")
	if err != nil {
		return err
	}
	defer h.Close()
	return runServe(ctx, h, cfgPath)
}

func maybeAutoSetupDesktop(ctx context.Context, eng *desktop.Engine) {
	if eng == nil {
		return
	}
	st, err := eng.Status(ctx)
	if err != nil || st == nil || !st.Virtual || !st.Installable || len(st.MissingDeps) == 0 {
		return
	}
	log.Printf("[desktop] headless virtual desktop missing deps (%s); attempting auto-install...", strings.Join(st.MissingDeps, ", "))
	setupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	res, err := eng.Setup(setupCtx)
	if err != nil {
		log.Printf("[desktop] auto-install failed: %v", err)
		return
	}
	if res != nil && !res.OK {
		log.Printf("[desktop] auto-install incomplete: %s", res.Error)
		return
	}
	log.Printf("[desktop] virtual desktop dependencies ready")
}

func runServe(parent context.Context, h *hub.Hub, cfgPath string) error {
	ensureBotHubInPath(h.Config().ProjectRoot)

	ctx, stop := context.WithCancel(parent)
	defer stop()

	log.Printf("[hub] loaded config %s (%d bots)", cfgPath, len(h.Bots()))
	for _, b := range h.Bots() {
		log.Printf("[hub] bot=%s workspace=%s model=%s", b.ID, b.Workspace, b.Model)
	}

	sched := scheduler.New(h)
	h.SetOnSchedulesChanged(sched.Sync)
	sched.Start(ctx)

	var httpSrv *channel.HTTPServer
	httpErr := make(chan error, 1)
	if h.Config().HTTP.IsEnabled() {
		httpSrv = channel.NewHTTPServer(h, h.Config().HTTP)
		httpSrv.SetScheduler(sched)
		go func() {
			if err := httpSrv.Start(); err != nil {
				log.Printf("[http] server error: %v", err)
				httpErr <- err
				stop()
			}
		}()
	}

	<-ctx.Done()
	log.Printf("[hub] shutting down...")

	if httpSrv != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = httpSrv.Shutdown(shutCtx)
		cancel()
	}
	sched.Wait()
	log.Printf("[hub] stopped")
	select {
	case err := <-httpErr:
		return err
	default:
		return nil
	}
}

func runChat(h *hub.Hub, botID string) {
	bot, ok := h.GetBot(botID)
	if !ok {
		log.Fatalf("bot %q not found", botID)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Connected to bot %q (%s)\n", bot.Name, bot.ID)
	fmt.Printf("Workspace: %s\n", bot.Workspace)
	fmt.Printf("Commands: /status, /reset, /bots, /ask <bot> <msg>, /exit\n\n")

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for {
		fmt.Printf("[%s] > ", bot.ID)
		if !sc.Scan() {
			fmt.Println()
			return
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line == "/exit" || line == "/quit" {
			return
		}

		res, err := h.HandleInput(ctx, bot.ID, line, hub.SendOptions{
			Source: "cli",
			OnToolUse: func(tool string) {
				fmt.Fprintf(os.Stderr, "  [tool] %s\n", tool)
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
			continue
		}
		fmt.Printf("\n%s\n", res.Text)
		if res.DurationMs > 0 {
			fmt.Printf("  (session=%s | %dms | $%.4f)\n", res.SessionID, res.DurationMs, res.Usage.CostUSD)
		}
		fmt.Println()
	}
}

func runStatus(h *hub.Hub, cfgPath string) {
	fmt.Printf("Config: %s\n\n", cfgPath)
	sts, err := h.Status("")
	if err != nil {
		log.Fatalf("status: %v", err)
	}
	for _, st := range sts {
		sess := "(none)"
		turns := 0
		cost := 0.0
		if st.Session != nil {
			sess = st.Session.SessionID
			turns = st.Session.TurnCount
			cost = st.Session.TotalCostUSD
		}
		fmt.Printf("- %s (%s)\n", st.ID, st.Name)
		fmt.Printf("  workspace: %s\n", st.Workspace)
		fmt.Printf("  model:     %s\n", st.Model)
		fmt.Printf("  session:   %s (turns=%d, cost=$%.4f)\n", sess, turns, cost)
	}
}

func ensureBotHubInPath(projectRoot string) {
	bin := config.ResolveBotHubBinary(projectRoot)
	if bin == "" {
		return
	}
	if _, err := os.Stat(bin); err != nil {
		return
	}
	dir := filepath.Clean(filepath.Dir(bin))
	cur := os.Getenv("PATH")
	for _, seg := range strings.Split(cur, string(os.PathListSeparator)) {
		if seg != "" && filepath.Clean(seg) == dir {
			return
		}
	}
	if cur == "" {
		_ = os.Setenv("PATH", dir)
	} else {
		_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+cur)
	}
}

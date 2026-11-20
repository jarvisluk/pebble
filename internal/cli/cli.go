// Package cli holds bot-hub's commands, shared by the CLI and the desktop app.
package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bot-hub/internal/channel"
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/mcp"
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

	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	cfgFlag := fs.String("config", "", "path to bots.json config file")
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
  bot-hub serve  [-config path]                    Start Web UI, HTTP API, Telegram pollers, and scheduler
  bot-hub chat   [-config path] [bot]              Interactive terminal chat with a bot
  bot-hub status [-config path]                    Show configured bots and persisted sessions
  bot-hub mcp    [--hub-url url] [--bot-id id]     Run stdio MCP server exposing bot management & messaging tools
  bot-hub connect [-listen addr] <tailcat-link>    Reach a remote bot-hub over Tailcat at a local URL`)
}

func runConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:3790", "local address to serve the remote bot-hub on")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage()
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("[tailcat] connecting...")
	err := tunnel.Forward(ctx, fs.Arg(0), *listen, func(u string) { log.Printf("[tailcat] connected: open %s", u) })
	if err != nil && ctx.Err() == nil {
		log.Fatalf("%v", err)
	}
}

func runMCP(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	hubURL := fs.String("hub-url", "http://127.0.0.1:3789", "running bot-hub HTTP base URL")
	botID := fs.String("bot-id", "", "caller bot ID")
	_ = fs.Parse(args)

	srv := mcp.NewServer(*hubURL, *botID)
	if err := srv.ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
		os.Exit(1)
	}
}

func load(root, cfgFlag string) (*hub.Hub, string, error) {
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

func runServe(parent context.Context, h *hub.Hub, cfgPath string) error {
	ctx, stop := context.WithCancel(parent)
	defer stop()

	log.Printf("[hub] loaded config %s (%d bots)", cfgPath, len(h.Bots()))
	for _, b := range h.Bots() {
		log.Printf("[hub] bot=%s workspace=%s model=%s", b.ID, b.Workspace, b.Model)
	}

	tg := channel.NewTelegramManager(h)
	tg.Start(ctx)

	sched := scheduler.New(h, tg)
	sched.Start(ctx)

	var httpSrv *channel.HTTPServer
	httpErr := make(chan error, 1)
	if h.Config().HTTP.IsEnabled() {
		httpSrv = channel.NewHTTPServer(h, h.Config().HTTP)
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
	tg.Wait()
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

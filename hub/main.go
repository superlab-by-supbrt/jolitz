// jolitz-hub is the optional team service of jolitz: one static
// binary that mirrors every member's memory/ into SQLite, serves
// it over HTTP (JSON API and a web UI) and knows who is on which
// host. It runs no command on any server and holds no secret:
// tokens are stored as SHA-256, memory files are refused when a
// value in them looks like a credential.
//
// Usage:
//
//	jolitz-hub serve [--config FILE] [--data DIR] [--listen ADDR] [--base-url URL]
//	jolitz-hub user add NAME --role viewer|operator|admin
//	jolitz-hub user list
//	jolitz-hub user rm NAME
//	jolitz-hub user rotate NAME
//	jolitz-hub version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/api"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/jobs"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/ui"
)

// version is set at build time:
// go build -ldflags "-X main.version=$(cat ../VERSION)".
var version = "dev"

const usage = `jolitz-hub: the optional team hub of jolitz.

Usage:
  jolitz-hub serve [--config FILE] [--data DIR] [--listen ADDR]
                   [--base-url URL]
  jolitz-hub user add NAME --role viewer|operator|admin
  jolitz-hub user list
  jolitz-hub user rm NAME
  jolitz-hub user rotate NAME
  jolitz-hub version

serve      Run the API, the web UI, /metrics and the jobs
           (retention, endoflife.date fetch, weekly summary).
           --config is hub.toml (default: <data>/hub.toml when
           it exists; see hub/hub.toml for every key), --data
           holds hub.db (default /var/lib/jolitz-hub), --listen
           is the address (default 127.0.0.1:7331; put TLS in
           front), --base-url is the public URL shown on the
           /api page. A flag given on the command line wins
           over the file.
user add   Create a user and print its token once. The hub
           stores only the token's SHA-256.
user list  Names, roles and creation dates.
user rm    Delete a user; its sessions end, its claims stay.
user rotate
           Replace a user's token and print the new one once.

Roles: viewer reads, operator also writes files, sessions,
claims and guard denials, admin also releases other people's
claims, ends their sessions and takes over claims.

Environment: JOLITZ_HUB_DATA replaces the --data default.
`

func main() {
	log.SetFlags(0)
	log.SetPrefix("jolitz-hub: ")
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "user":
		err = user(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("jolitz-hub " + version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		log.Fatal(err)
	}
}

var errUsage = errors.New("usage")

func defaultData() string {
	if d := os.Getenv("JOLITZ_HUB_DATA"); d != "" {
		return d
	}
	return "/var/lib/jolitz-hub"
}

func openStore(dir string) (*store.Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	st, err := store.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Join(dir, "hub.db"), err)
	}
	return st, nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := fs.String("data", defaultData(), "data directory holding hub.db")
	listen := fs.String("listen", "127.0.0.1:7331", "listen address")
	baseURL := fs.String("base-url", "", "public URL of the hub, for the /api page")
	cfgPath := fs.String("config", "", "hub.toml (default <data>/hub.toml when it exists)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	cfg, err := loadConfig(fs, *cfgPath, *data, *listen, *baseURL)
	if err != nil {
		return err
	}
	st, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	users, err := st.ListUsers()
	if err != nil {
		return err
	}
	if len(users) == 0 {
		log.Printf("no users yet: jolitz-hub user add NAME --role admin")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sched := jobs.New(st, cfg, log.Printf)
	go sched.Run(ctx)
	apiCfg := api.Config{Store: st, Version: version, BaseURL: cfg.BaseURL, Settings: cfg}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", api.New(apiCfg))
	mux.Handle(api.MetricsPath, api.Metrics(apiCfg))
	mux.Handle("/", ui.New(ui.Config{Store: st, Version: version, BaseURL: cfg.BaseURL, Settings: cfg, Scheduler: sched}))
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errc := make(chan error, 1)
	go func() {
		src := "defaults (no hub.toml)"
		if cfg.Path != "" {
			src = cfg.Path
		}
		var names []string
		for _, j := range sched.Jobs {
			names = append(names, j.Name)
		}
		log.Printf("version %s, data %s, config %s, jobs %s, listening on http://%s", version, cfg.DataDir, src, strings.Join(names, " "), cfg.Listen)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Printf("shutting down")
		return srv.Shutdown(shutdown)
	}
}

// loadConfig reads hub.toml and applies the command line on top.
// The file is --config, else <data>/hub.toml when it exists, else
// none. A flag given explicitly (or JOLITZ_HUB_DATA) wins over
// the file; the file wins over a flag's default.
func loadConfig(fs *flag.FlagSet, path, data, listen, baseURL string) (*config.Config, error) {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if os.Getenv("JOLITZ_HUB_DATA") != "" {
		set["data"] = true
	}
	if path == "" {
		if candidate := filepath.Join(data, "hub.toml"); fileExists(candidate) {
			path = candidate
		}
	}
	cfg := config.Default()
	if path != "" {
		var err error
		if cfg, err = config.Load(path); err != nil {
			return nil, err
		}
	}
	if set["data"] || path == "" {
		cfg.DataDir = data
	}
	if set["listen"] {
		cfg.Listen = listen
	}
	if set["base-url"] {
		cfg.BaseURL = baseURL
	}
	return cfg, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// logRequests writes one line per request: method, path, status.
// Never the query string (a token must not land in a log) and
// never a body.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: 200}
		start := time.Now()
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %dms", r.Method, r.URL.Path, sw.code, time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func user(args []string) error {
	if len(args) < 1 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("user "+sub, flag.ContinueOnError)
	data := fs.String("data", defaultData(), "data directory holding hub.db")
	role := fs.String("role", "", "viewer, operator or admin")
	// Flags may follow the name: "user add alice --role admin".
	var names []string
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return errUsage
		}
		rest = fs.Args()
		if len(rest) > 0 {
			names = append(names, rest[0])
			rest = rest[1:]
		}
	}
	st, err := openStore(*data)
	if err != nil {
		return err
	}
	defer st.Close()
	switch sub {
	case "add":
		if len(names) != 1 || *role == "" {
			return errUsage
		}
		token, err := st.AddUser(names[0], *role)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "user %s (%s) created. The token, shown once:\n", names[0], *role)
		fmt.Println(token)
		fmt.Fprintf(os.Stderr, "Store it as one line 'Authorization: Bearer <token>' in a mode-600 file, e.g.\n  install -m 600 /dev/null ~/.config/jolitz/hub-token\nthen paste it there with your editor; curl -H @file reads it.\n")
	case "list":
		if len(names) != 0 {
			return errUsage
		}
		users, err := st.ListUsers()
		if err != nil {
			return err
		}
		for _, u := range users {
			fmt.Printf("%-24s %-9s %s\n", u.Name, u.Role, u.CreatedAt.UTC().Format("2006-01-02 15:04"))
		}
	case "rm":
		if len(names) != 1 {
			return errUsage
		}
		if err := st.RemoveUser(names[0]); err != nil {
			return fmt.Errorf("user %s: %w", names[0], err)
		}
		fmt.Fprintf(os.Stderr, "user %s removed\n", names[0])
	case "rotate":
		if len(names) != 1 {
			return errUsage
		}
		token, err := st.RotateToken(names[0])
		if err != nil {
			return fmt.Errorf("user %s: %w", names[0], err)
		}
		fmt.Fprintf(os.Stderr, "token of %s replaced. The new one, shown once:\n", names[0])
		fmt.Println(token)
	default:
		return errUsage
	}
	return nil
}

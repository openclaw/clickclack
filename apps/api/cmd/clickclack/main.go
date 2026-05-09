package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/config"
	"github.com/openclaw/clickclack/apps/api/internal/httpapi"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		return serve(os.Args[2:])
	case "migrate":
		return migrate(os.Args[2:])
	case "admin":
		return admin(os.Args[2:])
	case "backup":
		return backup(os.Args[2:])
	case "export":
		return exportData(os.Args[2:])
	case "version":
		fmt.Printf("clickclack %s (%s, %s)\n", version, commit, date)
		return nil
	default:
		return client(os.Args[1:])
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	flags.String("addr", ":8080", "HTTP listen address")
	flags.String("data", "./data", "data directory")
	flags.String("db", "", "database URL")
	configPath := flags.String("config", "", "config file")
	flags.Bool("dev-bootstrap", true, "create a local owner/workspace/channel if no user exists")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	applyFlagOverrides(flags, &cfg)
	url := resolveDB(cfg.Data, cfg.DB)
	if err := ensureDirs(cfg.Data); err != nil {
		return err
	}
	st, err := sqlitestore.Open(url)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if cfg.DevBootstrap {
		user, err := st.EnsureBootstrap(ctx, "Local Captain", "local@clickclack.chat")
		if err != nil {
			return err
		}
		log.Printf("dev auth user: %s (%s)", user.DisplayName, user.ID)
	}
	var pushNotifier httpapi.PushNotifier
	if cfg.PushoverAPIToken != "" {
		pushNotifier = httpapi.NewPushoverNotifier(cfg.PushoverAPIToken)
	}
	log.Printf("ClickClack listening on %s", displayURL(cfg.Addr))
	server := httpapi.New(st, realtime.NewHub(), httpapi.Options{
		UploadDir:      filepath.Join(cfg.Data, "uploads"),
		DisableDevAuth: !cfg.DevBootstrap,
		GitHubOAuth: httpapi.GitHubOAuthConfig{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			PublicURL:    cfg.PublicURL,
			AllowedOrg:   cfg.GitHubAllowedOrg,
		},
		PushNotifier: pushNotifier,
	})
	return httpapi.ListenAndServe(ctx, cfg.Addr, server.Handler())
}

func migrate(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ExitOnError)
	data := flags.String("data", "./data", "data directory")
	dbURL := flags.String("db", "", "database URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := ensureDirs(*data); err != nil {
		return err
	}
	st, err := sqlitestore.Open(resolveDB(*data, *dbURL))
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Migrate(context.Background())
}

func admin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("admin requires a subcommand")
	}
	switch args[0] {
	case "bootstrap":
		flags := flag.NewFlagSet("admin bootstrap", flag.ExitOnError)
		data := flags.String("data", "./data", "data directory")
		dbURL := flags.String("db", "", "database URL")
		name := flags.String("name", "Owner", "owner display name")
		email := flags.String("email", "", "owner email")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := ensureDirs(*data); err != nil {
			return err
		}
		st, err := sqlitestore.Open(resolveDB(*data, *dbURL))
		if err != nil {
			return err
		}
		defer st.Close()
		ctx := context.Background()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		user, err := st.EnsureBootstrap(ctx, *name, *email)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", user.ID)
		return nil
	case "user":
		if len(args) < 2 || args[1] != "create" {
			return fmt.Errorf("usage: clickclack admin user create --name NAME --email EMAIL")
		}
		flags := flag.NewFlagSet("admin user create", flag.ExitOnError)
		data := flags.String("data", "./data", "data directory")
		dbURL := flags.String("db", "", "database URL")
		name := flags.String("name", "Local User", "display name")
		email := flags.String("email", "", "email")
		workspaceID := flags.String("workspace", "", "workspace id to join as member")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		st, err := sqlitestore.Open(resolveDB(*data, *dbURL))
		if err != nil {
			return err
		}
		defer st.Close()
		if err := st.Migrate(context.Background()); err != nil {
			return err
		}
		user, err := st.CreateUser(context.Background(), store.CreateUserInput{DisplayName: *name, Email: *email})
		if err != nil {
			return err
		}
		if *workspaceID != "" {
			if err := st.AddWorkspaceMember(context.Background(), *workspaceID, user.ID, "member"); err != nil {
				return err
			}
		}
		fmt.Printf("%s\n", user.ID)
		return nil
	case "invite":
		if len(args) < 2 || args[1] != "create" {
			return fmt.Errorf("usage: clickclack admin invite create --workspace WORKSPACE_ID")
		}
		flags := flag.NewFlagSet("admin invite create", flag.ExitOnError)
		data := flags.String("data", "./data", "data directory")
		dbURL := flags.String("db", "", "database URL")
		workspaceID := flags.String("workspace", "", "workspace id")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if *workspaceID == "" {
			return fmt.Errorf("--workspace is required")
		}
		st, err := sqlitestore.Open(resolveDB(*data, *dbURL))
		if err != nil {
			return err
		}
		defer st.Close()
		ctx := context.Background()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		user, err := st.FirstUser(ctx)
		if err != nil {
			return err
		}
		invite, err := st.CreateInvite(ctx, *workspaceID, user.ID)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", invite.Token)
		return nil
	case "magic-link":
		if len(args) < 2 || args[1] != "create" {
			return fmt.Errorf("usage: clickclack admin magic-link create --email EMAIL [--name NAME]")
		}
		flags := flag.NewFlagSet("admin magic-link create", flag.ExitOnError)
		data := flags.String("data", "./data", "data directory")
		dbURL := flags.String("db", "", "database URL")
		email := flags.String("email", "", "email")
		name := flags.String("name", "", "display name")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		st, err := sqlitestore.Open(resolveDB(*data, *dbURL))
		if err != nil {
			return err
		}
		defer st.Close()
		ctx := context.Background()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		link, err := st.CreateMagicLink(ctx, *email, *name)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", link.Token)
		return nil
	default:
		return fmt.Errorf("unknown admin subcommand %q", args[0])
	}
}

func backup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ExitOnError)
	data := flags.String("data", "./data", "data directory")
	dbURL := flags.String("db", "", "database URL")
	out := flags.String("out", "", "backup SQLite path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	st, err := sqlitestore.Open(resolveDB(*data, *dbURL))
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Backup(context.Background(), *out)
}

func exportData(args []string) error {
	flags := flag.NewFlagSet("export", flag.ExitOnError)
	data := flags.String("data", "./data", "data directory")
	dbURL := flags.String("db", "", "database URL")
	out := flags.String("out", "-", "JSON output path or '-'")
	if err := flags.Parse(args); err != nil {
		return err
	}
	st, err := sqlitestore.Open(resolveDB(*data, *dbURL))
	if err != nil {
		return err
	}
	defer st.Close()
	var writer *os.File
	if *out == "-" {
		writer = os.Stdout
	} else {
		writer, err = os.Create(*out)
		if err != nil {
			return err
		}
		defer writer.Close()
	}
	return st.ExportJSON(context.Background(), writer)
}

func resolveDB(data, dbURL string) string {
	if dbURL != "" {
		return dbURL
	}
	return "sqlite://" + filepath.Join(data, "clickclack.db")
}

func applyFlagOverrides(flags *flag.FlagSet, cfg *config.Config) {
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "addr":
			cfg.Addr = f.Value.String()
		case "data":
			cfg.Data = f.Value.String()
		case "db":
			cfg.DB = f.Value.String()
		case "dev-bootstrap":
			cfg.DevBootstrap = f.Value.String() == "true"
		}
	})
}

func ensureDirs(data string) error {
	for _, dir := range []string{data, filepath.Join(data, "uploads"), filepath.Join(data, "logs")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func displayURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}

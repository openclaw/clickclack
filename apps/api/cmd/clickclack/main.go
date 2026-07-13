package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/authpolicy"
	"github.com/openclaw/clickclack/apps/api/internal/config"
	"github.com/openclaw/clickclack/apps/api/internal/fakeco"
	"github.com/openclaw/clickclack/apps/api/internal/httpapi"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	postgresstore "github.com/openclaw/clickclack/apps/api/internal/store/postgres"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
	"github.com/openclaw/clickclack/apps/api/internal/uploadstore"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type databaseStore interface {
	store.Store
	Backup(ctx context.Context, outPath string) error
	ExportJSON(ctx context.Context, writer io.Writer) error
	PruneEvents(ctx context.Context, workspaceID string, keepLatest int, before string) (int64, error)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	return runArgs(os.Args)
}

func runArgs(args []string) error {
	cmd, cmdArgs, clientArgs := dispatchArgs(args)
	switch cmd {
	case "serve":
		return serve(cmdArgs)
	case "migrate":
		return migrate(cmdArgs)
	case "admin":
		return admin(cmdArgs)
	case "backup":
		return backup(cmdArgs)
	case "export":
		return exportData(cmdArgs)
	case "version":
		fmt.Printf("clickclack %s (%s, %s)\n", version, commit, date)
		return nil
	default:
		return client(clientArgs)
	}
}

func dispatchArgs(args []string) (string, []string, []string) {
	if len(args) <= 1 {
		return "serve", nil, nil
	}
	return args[1], args[2:], args[1:]
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	flags.String("addr", ":8080", "HTTP listen address")
	flags.String("data", defaultData(), "data directory")
	flags.String("db", defaultDB(), "database URL")
	flags.String("uploads", defaultUploads(), "upload storage URL")
	flags.String("environment", "", "deployment environment label")
	configPath := flags.String("config", "", "config file")
	flags.Bool("dev-bootstrap", false, "create a local owner/workspace/channel if no user exists")
	flags.Bool("metrics-enabled", false, "expose metadata-only Prometheus metrics at /metrics")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	applyFlagOverrides(flags, &cfg)
	if err := cfg.ValidateServe(); err != nil {
		return err
	}
	cookieNames, err := authpolicy.NewCookieNames(cfg.CookieNamespace, cfg.PublicURL)
	if err != nil {
		return err
	}
	url := resolveDB(cfg.Data, cfg.DB)
	if err := ensureDirs(cfg.Data); err != nil {
		return err
	}
	uploads, err := openUploadStorage(cfg)
	if err != nil {
		return err
	}
	st, err := openStore(url)
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
		UploadStorage:  uploads,
		DisableDevAuth: !cfg.DevBootstrap,
		CookieNames:    cookieNames,
		GitHubOAuth: httpapi.GitHubOAuthConfig{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			PublicURL:    cfg.PublicURL,
			AllowedOrg:   cfg.GitHubAllowedOrg,
			ModeratorOrg: cfg.GitHubModeratorOrg,
		},
		PushNotifier:   pushNotifier,
		MetricsEnabled: cfg.MetricsEnabled,
		Environment:    cfg.Environment,
		Version:        version,
		Commit:         commit,
	})
	if uploads != nil {
		if err := server.CleanupPendingUploadObjects(ctx, 0); err != nil {
			log.Printf("pending upload cleanup retry failed: %v", err)
		}
	}
	return httpapi.ListenAndServe(ctx, cfg.Addr, server.Handler())
}

func migrate(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ExitOnError)
	data := flags.String("data", defaultData(), "data directory")
	dbURL := flags.String("db", defaultDB(), "database URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := ensureDirs(*data); err != nil {
		return err
	}
	st, err := openStore(resolveDB(*data, *dbURL))
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
		data := flags.String("data", defaultData(), "data directory")
		dbURL := flags.String("db", defaultDB(), "database URL")
		name := flags.String("name", "Owner", "owner display name")
		email := flags.String("email", "", "owner email")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := ensureDirs(*data); err != nil {
			return err
		}
		st, err := openStore(resolveDB(*data, *dbURL))
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
	case "fakeco":
		if len(args) < 2 || args[1] != "seed" {
			return fmt.Errorf("usage: clickclack admin fakeco seed --environment fakeco [--data PATH] [--db URL]")
		}
		flags := flag.NewFlagSet("admin fakeco seed", flag.ExitOnError)
		data := flags.String("data", defaultData(), "data directory")
		dbURL := flags.String("db", defaultDB(), "database URL")
		environment := flags.String("environment", os.Getenv("CLICKCLACK_ENVIRONMENT"), "deployment environment confirmation")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if *environment != "fakeco" {
			return errors.New("refusing FakeCo seed: --environment or CLICKCLACK_ENVIRONMENT must equal \"fakeco\"")
		}
		if err := ensureDirs(*data); err != nil {
			return err
		}
		st, err := openStore(resolveDB(*data, *dbURL))
		if err != nil {
			return err
		}
		defer st.Close()
		ctx := context.Background()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		manifest, err := fakeco.Seed(ctx, st)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(manifest)
	case "user":
		if len(args) < 2 || args[1] != "create" {
			return fmt.Errorf("usage: clickclack admin user create --name NAME --email EMAIL")
		}
		flags := flag.NewFlagSet("admin user create", flag.ExitOnError)
		data := flags.String("data", defaultData(), "data directory")
		dbURL := flags.String("db", defaultDB(), "database URL")
		name := flags.String("name", "Local User", "display name")
		email := flags.String("email", "", "email")
		workspaceID := flags.String("workspace", "", "workspace id to join as member")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		st, err := openStore(resolveDB(*data, *dbURL))
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
		data := flags.String("data", defaultData(), "data directory")
		dbURL := flags.String("db", defaultDB(), "database URL")
		workspaceID := flags.String("workspace", "", "workspace id")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if *workspaceID == "" {
			return fmt.Errorf("--workspace is required")
		}
		st, err := openStore(resolveDB(*data, *dbURL))
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
	case "bot":
		if len(args) < 2 || args[1] != "create" {
			return fmt.Errorf("usage: clickclack admin bot create --workspace WORKSPACE_ID --created-by USER_ID --name NAME [--owner USER_ID] [--scopes bot:write]")
		}
		flags := flag.NewFlagSet("admin bot create", flag.ExitOnError)
		data := flags.String("data", defaultData(), "data directory")
		dbURL := flags.String("db", defaultDB(), "database URL")
		workspaceID := flags.String("workspace", "", "workspace id")
		ownerID := flags.String("owner", "", "human owner user id")
		name := flags.String("name", "", "bot display name")
		handle := flags.String("handle", "", "bot handle")
		avatarURL := flags.String("avatar-url", "", "bot avatar URL")
		tokenName := flags.String("token-name", "default", "bot token label")
		scopes := flags.String("scopes", "bot:write", "comma-separated scopes or bundle")
		createdBy := flags.String("created-by", "", "human creator user id")
		plain := flags.Bool("plain", false, "print only the raw bot token")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if strings.TrimSpace(*workspaceID) == "" {
			return fmt.Errorf("--workspace is required")
		}
		if strings.TrimSpace(*createdBy) == "" {
			return fmt.Errorf("--created-by is required")
		}
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		st, err := openStore(resolveDB(*data, *dbURL))
		if err != nil {
			return err
		}
		defer st.Close()
		ctx := context.Background()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
			WorkspaceID: *workspaceID,
			OwnerUserID: *ownerID,
			DisplayName: *name,
			Handle:      *handle,
			AvatarURL:   *avatarURL,
			TokenName:   *tokenName,
			Scopes:      strings.Split(*scopes, ","),
			CreatedBy:   *createdBy,
		})
		if err != nil {
			return err
		}
		if *plain {
			fmt.Printf("%s\n", token.Token)
			return nil
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"bot": bot, "bot_token": token, "token": token.Token})
	case "events":
		if len(args) < 2 || args[1] != "prune" {
			return fmt.Errorf("usage: clickclack admin events prune --workspace WORKSPACE_ID [--older-than-days DAYS | --before RFC3339] [--keep-latest N]")
		}
		flags := flag.NewFlagSet("admin events prune", flag.ExitOnError)
		data := flags.String("data", defaultData(), "data directory")
		dbURL := flags.String("db", defaultDB(), "database URL")
		workspaceID := flags.String("workspace", "", "workspace id")
		olderThanDays := flags.Int("older-than-days", 0, "delete events older than this many days")
		before := flags.String("before", "", "delete events created before this RFC3339 timestamp")
		keepLatest := flags.Int("keep-latest", 0, "always keep the latest N events in the workspace")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if *workspaceID == "" {
			return fmt.Errorf("--workspace is required")
		}
		if *olderThanDays < 0 {
			return fmt.Errorf("--older-than-days must be non-negative")
		}
		if *keepLatest < 0 {
			return fmt.Errorf("--keep-latest must be non-negative")
		}
		if *olderThanDays > 0 && strings.TrimSpace(*before) != "" {
			return fmt.Errorf("--older-than-days and --before are mutually exclusive")
		}
		cutoff := strings.TrimSpace(*before)
		if cutoff != "" {
			parsed, err := time.Parse(time.RFC3339Nano, cutoff)
			if err != nil {
				return fmt.Errorf("--before must be RFC3339: %w", err)
			}
			cutoff = parsed.UTC().Format(time.RFC3339Nano)
		}
		if *olderThanDays > 0 {
			cutoff = time.Now().UTC().Add(-time.Duration(*olderThanDays) * 24 * time.Hour).Format(time.RFC3339Nano)
		}
		if cutoff == "" && *keepLatest == 0 {
			return fmt.Errorf("provide --older-than-days, --before, or --keep-latest")
		}
		if err := ensureDirs(*data); err != nil {
			return err
		}
		st, err := openStore(resolveDB(*data, *dbURL))
		if err != nil {
			return err
		}
		defer st.Close()
		ctx := context.Background()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		pruned, err := st.PruneEvents(ctx, *workspaceID, *keepLatest, cutoff)
		if err != nil {
			return err
		}
		fmt.Printf("pruned %d events\n", pruned)
		return nil
	case "magic-link":
		if len(args) < 2 || args[1] != "create" {
			return fmt.Errorf("usage: clickclack admin magic-link create --email EMAIL [--name NAME]")
		}
		flags := flag.NewFlagSet("admin magic-link create", flag.ExitOnError)
		data := flags.String("data", defaultData(), "data directory")
		dbURL := flags.String("db", defaultDB(), "database URL")
		email := flags.String("email", "", "email")
		name := flags.String("name", "", "display name")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		st, err := openStore(resolveDB(*data, *dbURL))
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
	data := flags.String("data", defaultData(), "data directory")
	dbURL := flags.String("db", defaultDB(), "database URL")
	out := flags.String("out", "", "backup SQLite path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	st, err := openStore(resolveDB(*data, *dbURL))
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Backup(context.Background(), *out)
}

func exportData(args []string) error {
	flags := flag.NewFlagSet("export", flag.ExitOnError)
	data := flags.String("data", defaultData(), "data directory")
	dbURL := flags.String("db", defaultDB(), "database URL")
	out := flags.String("out", "-", "JSON output path or '-'")
	if err := flags.Parse(args); err != nil {
		return err
	}
	st, err := openStore(resolveDB(*data, *dbURL))
	if err != nil {
		return err
	}
	defer st.Close()
	var writer *os.File
	if *out == "-" {
		writer = os.Stdout
	} else {
		dir := filepath.Dir(*out)
		writer, err = os.CreateTemp(dir, "."+filepath.Base(*out)+".tmp-*")
		if err != nil {
			return err
		}
		tmpName := writer.Name()
		defer os.Remove(tmpName)
		if err := st.ExportJSON(context.Background(), writer); err != nil {
			writer.Close()
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		return os.Rename(tmpName, *out)
	}
	return st.ExportJSON(context.Background(), writer)
}

func resolveDB(data, dbURL string) string {
	if dbURL != "" {
		return dbURL
	}
	return "sqlite://" + filepath.Join(data, "clickclack.db")
}

func defaultData() string {
	if value := os.Getenv("CLICKCLACK_DATA"); value != "" {
		return value
	}
	return "./data"
}

func defaultDB() string {
	return os.Getenv("CLICKCLACK_DB")
}

func defaultUploads() string {
	return os.Getenv("CLICKCLACK_UPLOADS")
}

func openStore(dbURL string) (databaseStore, error) {
	switch {
	case strings.HasPrefix(dbURL, "postgres://"), strings.HasPrefix(dbURL, "postgresql://"):
		return postgresstore.Open(dbURL)
	default:
		return sqlitestore.Open(dbURL)
	}
}

func openUploadStorage(cfg config.Config) (uploadstore.Store, error) {
	uploads := cfg.Uploads
	if uploads == "" {
		return uploadstore.NewLocal(filepath.Join(cfg.Data, "uploads")), nil
	}
	if strings.HasPrefix(uploads, "r2://") {
		u, err := url.Parse(uploads)
		if err != nil {
			return nil, err
		}
		return uploadstore.NewR2(uploadstore.R2Config{
			AccountID:       cfg.R2AccountID,
			AccessKeyID:     cfg.R2AccessKeyID,
			SecretAccessKey: cfg.R2SecretAccessKey,
			Bucket:          u.Host,
			Prefix:          strings.TrimLeft(u.Path, "/"),
			Endpoint:        cfg.R2Endpoint,
		})
	}
	if strings.HasPrefix(uploads, "file://") {
		u, err := url.Parse(uploads)
		if err != nil {
			return nil, err
		}
		return uploadstore.NewLocal(u.Path), nil
	}
	return uploadstore.NewLocal(uploads), nil
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
		case "uploads":
			cfg.Uploads = f.Value.String()
		case "environment":
			cfg.Environment = f.Value.String()
		case "dev-bootstrap":
			cfg.DevBootstrap = f.Value.String() == "true"
		case "metrics-enabled":
			cfg.MetricsEnabled = f.Value.String() == "true"
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

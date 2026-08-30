package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func adminIdentity(args []string) error {
	if len(args) == 0 || args[0] != "sync" {
		return errors.New("usage: clickclack admin identity sync --source ORIGIN [--file profiles.json | --file -] [--data PATH] [--db URL]")
	}
	flags := flag.NewFlagSet("admin identity sync", flag.ContinueOnError)
	data := flags.String("data", defaultData(), "data directory")
	dbURL := flags.String("db", defaultDB(), "database URL")
	source := flags.String("source", "", "authenticated OpenClaw origin owning the exported profiles")
	file := flags.String("file", "-", "users.list JSON export, or - for stdin")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("identity sync does not accept positional arguments")
	}
	reader := os.Stdin
	if *file != "-" {
		var err error
		reader, err = os.Open(*file)
		if err != nil {
			return err
		}
		defer reader.Close()
	}
	const maxImportBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(reader, maxImportBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxImportBytes {
		return errors.New("profile export exceeds 4 MiB")
	}
	var document struct {
		Profiles []store.IdentitySyncProfile `json:"profiles"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return err
	}
	input, err := store.NormalizeIdentitySync(store.IdentitySyncInput{Source: *source, Profiles: document.Profiles})
	if err != nil {
		return err
	}
	st, err := openStore(resolveDB(*data, *dbURL))
	if err != nil {
		return err
	}
	defer st.Close()
	// Sync consumes an existing deployment; it never migrates or provisions users.
	report, err := st.SyncIdentities(context.Background(), input)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

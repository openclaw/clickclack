package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/openclaw/clickclack/apps/api/internal/passwordauth"
	"golang.org/x/term"
)

// adminUserSetPassword enables, replaces, or clears the local password for one
// account. The secret is read from a terminal prompt or piped stdin and never
// from a flag, because process arguments are visible to every other process on
// the host.
func adminUserSetPassword(args []string) error {
	flags := flag.NewFlagSet("admin user set-password", flag.ExitOnError)
	data := flags.String("data", defaultData(), "data directory")
	dbURL := flags.String("db", defaultDB(), "database URL")
	email := flags.String("email", "", "account email or handle")
	userID := flags.String("user", "", "account user id")
	clear := flags.Bool("clear", false, "remove the password and disable password sign-in")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if (*email == "") == (*userID == "") {
		return errors.New("usage: clickclack admin user set-password (--email EMAIL | --user USER_ID) [--clear]")
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
	id := strings.TrimSpace(*userID)
	if id == "" {
		login, err := st.GetPasswordLogin(ctx, *email)
		if err != nil {
			return fmt.Errorf("no account found for %q: %w", *email, err)
		}
		id = login.User.ID
	}
	if *clear {
		if err := st.ClearUserPassword(ctx, id); err != nil {
			return err
		}
		fmt.Printf("password cleared for %s\n", id)
		return nil
	}
	password, err := readAdminPassword()
	if err != nil {
		return err
	}
	hash, err := passwordauth.Hash(ctx, password)
	if err != nil {
		return err
	}
	if err := st.SetUserPassword(ctx, id, hash); err != nil {
		return err
	}
	fmt.Printf("password set for %s\n", id)
	return nil
}

// readAdminPassword prompts twice with echo disabled on a terminal, and
// otherwise reads a single piped value so the command stays scriptable.
func readAdminPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Allow every code point plus an optional CRLF, then detect overflow
		// before removing the line ending so a truncated prefix is never hashed.
		const maxBytes = passwordauth.MaxPasswordLength*utf8.UTFMax + len("\r\n")
		body, err := io.ReadAll(io.LimitReader(os.Stdin, int64(maxBytes)+1))
		if err != nil {
			return "", err
		}
		if len(body) > maxBytes {
			return "", passwordauth.ErrPasswordTooLong
		}
		password, hasLF := strings.CutSuffix(string(body), "\n")
		if hasLF {
			password = strings.TrimSuffix(password, "\r")
		}
		return password, nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("passwords did not match")
	}
	return string(first), nil
}

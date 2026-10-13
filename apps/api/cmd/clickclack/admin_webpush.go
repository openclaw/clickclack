package main

import (
	"flag"
	"fmt"

	"github.com/openclaw/clickclack/apps/api/internal/webpush"
)

// adminWebPushKeygen prints one fresh VAPID key pair in the form the server
// reads it. The pair is generated in memory and never stored: copy it into the
// deployment's configuration, and treat the private key like any other secret.
func adminWebPushKeygen(args []string) error {
	flags := flag.NewFlagSet("admin webpush keygen", flag.ExitOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	publicKey, privateKey, err := webpush.GenerateKeys()
	if err != nil {
		return err
	}
	fmt.Printf("CLICKCLACK_WEBPUSH_VAPID_PUBLIC_KEY=%s\n", publicKey)
	fmt.Printf("CLICKCLACK_WEBPUSH_VAPID_PRIVATE_KEY=%s\n", privateKey)
	return nil
}

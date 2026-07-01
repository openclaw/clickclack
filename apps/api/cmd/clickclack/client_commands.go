package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (c apiClient) login(args []string) error {
	opts := c.opts
	flags := flag.NewFlagSet("login", flag.ExitOnError)
	addClientFlags(flags, &opts)
	magicToken := flags.String("magic-token", "", "magic-link token to consume")
	storeCreds := flags.Bool("store", true, "store returned session token")
	noStore := flags.Bool("no-store", false, "do not store returned session token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	c = c.withOptions(opts, false)
	if strings.TrimSpace(*magicToken) == "" {
		return errors.New("--magic-token is required")
	}
	var result struct {
		User    store.User    `json:"user"`
		Session store.Session `json:"session"`
		Token   string        `json:"token"`
	}
	if err := c.doJSON(context.Background(), http.MethodPost, "/api/auth/magic/consume", map[string]string{"token": *magicToken}, &result); err != nil {
		return err
	}
	if *noStore {
		*storeCreds = false
	}
	if *storeCreds {
		cfg := clientConfig{Server: c.opts.Server, Token: result.Token, Workspace: c.opts.Workspace, Channel: c.opts.Channel}
		if err := saveClientConfig(cfg); err != nil {
			return err
		}
	}
	return c.write(map[string]any{"user": result.User, "session": result.Session, "token": result.Token}, result.Token, fmt.Sprintf("logged in as %s\n", result.User.DisplayName))
}

func (c apiClient) logout(args []string) error {
	opts := c.opts
	flags := flag.NewFlagSet("logout", flag.ExitOnError)
	addClientFlags(flags, &opts)
	if err := flags.Parse(args); err != nil {
		return err
	}
	path, err := clientConfigPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	c = c.withOptions(opts, false)
	return c.write(map[string]bool{"logged_out": true}, "logged_out", "logged out\n")
}

func (c apiClient) whoami(args []string) error {
	opts := c.opts
	flags := flag.NewFlagSet("whoami", flag.ExitOnError)
	addClientFlags(flags, &opts)
	if err := flags.Parse(args); err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	var result struct {
		User store.User `json:"user"`
	}
	if err := c.get("/api/me", &result); err != nil {
		return err
	}
	return c.write(result, result.User.ID, fmt.Sprintf("%s\t%s\n", result.User.ID, result.User.DisplayName))
}

func (c apiClient) status(args []string) error {
	opts := c.opts
	flags := flag.NewFlagSet("status", flag.ExitOnError)
	addClientFlags(flags, &opts)
	if err := flags.Parse(args); err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	user, err := c.currentUser()
	if err != nil {
		return err
	}
	workspace, workspaceErr := c.resolveWorkspace()
	if workspaceErr != nil && strings.TrimSpace(c.opts.Workspace) != "" {
		return workspaceErr
	}
	channel, channelErr := c.resolveChannel()
	if channelErr != nil && strings.TrimSpace(c.opts.Channel) != "" {
		return channelErr
	}
	return c.write(map[string]any{"server": c.opts.Server, "user": user, "workspace": workspace, "channel": channel}, "", fmt.Sprintf("server\t%s\nuser\t%s\nworkspace\t%s\nchannel\t%s\n", c.opts.Server, user.ID, workspace.ID, channel.ID))
}

func (c apiClient) workspaces(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: clickclack workspaces list")
	}
	opts := c.opts
	flags := flag.NewFlagSet("workspaces list", flag.ExitOnError)
	addClientFlags(flags, &opts)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	items, err := c.listWorkspaces()
	if err != nil {
		return err
	}
	if c.opts.JSON {
		return writeJSON(os.Stdout, map[string]any{"workspaces": items})
	}
	for _, item := range items {
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", item.ID, item.Slug, item.Name)
	}
	return nil
}

func (c apiClient) channels(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: clickclack channels list [--workspace WORKSPACE]")
	}
	opts := c.opts
	flags := flag.NewFlagSet("channels list", flag.ExitOnError)
	addClientFlags(flags, &opts)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	workspace, err := c.resolveWorkspace()
	if err != nil {
		return err
	}
	items, err := c.listChannels(workspace.ID)
	if err != nil {
		return err
	}
	if c.opts.JSON {
		return writeJSON(os.Stdout, map[string]any{"workspace": workspace, "channels": items})
	}
	for _, item := range items {
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", item.ID, item.Name, item.Kind)
	}
	return nil
}

func (c apiClient) messages(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: clickclack messages <send|list>")
	}
	switch args[0] {
	case "send":
		return c.send(args[1:])
	case "list":
		return c.messagesList(args[1:])
	default:
		return fmt.Errorf("unknown messages command %q", args[0])
	}
}

func (c apiClient) send(args []string) error {
	opts := c.opts
	bodyFromFlag, stdin, file, replyTo, rest, err := parseBodyCommand("send", args, &opts)
	if err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	body, err := resolveBody(*bodyFromFlag, *file, *stdin, rest)
	if err != nil {
		return err
	}
	channel, err := c.resolveChannel()
	if err != nil {
		return err
	}
	var result struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}
	payload := messagePayload(body, *replyTo)
	if err := c.doJSON(context.Background(), http.MethodPost, "/api/channels/"+url.PathEscape(channel.ID)+"/messages", payload, &result); err != nil {
		return err
	}
	return c.write(result, result.Message.ID, fmt.Sprintf("sent %s to #%s\n", result.Message.ID, channel.Name))
}

func (c apiClient) messagesList(args []string) error {
	opts := c.opts
	flags := flag.NewFlagSet("messages list", flag.ExitOnError)
	addClientFlags(flags, &opts)
	limit := flags.Int("limit", 20, "message limit")
	afterSeq := flags.Int64("after-seq", 0, "only list messages after this channel sequence")
	if err := flags.Parse(args); err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	channel, err := c.resolveChannel()
	if err != nil {
		return err
	}
	var result struct {
		Messages []store.Message `json:"messages"`
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(*limit))
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "after-seq" {
			values.Set("after_seq", strconv.FormatInt(*afterSeq, 10))
		}
	})
	path := fmt.Sprintf("/api/channels/%s/messages?%s", url.PathEscape(channel.ID), values.Encode())
	if err := c.get(path, &result); err != nil {
		return err
	}
	if c.opts.JSON {
		return writeJSON(os.Stdout, map[string]any{"channel": channel, "messages": result.Messages})
	}
	for _, msg := range result.Messages {
		author := msg.AuthorID
		if msg.Author != nil {
			author = msg.Author.DisplayName
		}
		seq := int64(0)
		if msg.ChannelSeq != nil {
			seq = *msg.ChannelSeq
		}
		fmt.Fprintf(os.Stdout, "%d\t%s\t%s\t%s\n", seq, msg.ID, author, msg.Body)
	}
	return nil
}

func (c apiClient) threads(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: clickclack threads <open|reply> <message-id> [body]")
	}
	switch args[0] {
	case "open":
		return c.threadOpen(args[1], args[2:])
	case "reply":
		return c.threadReply(args[1], args[2:])
	default:
		return fmt.Errorf("unknown threads command %q", args[0])
	}
}

func (c apiClient) threadOpen(messageID string, args []string) error {
	opts := c.opts
	flags := flag.NewFlagSet("threads open", flag.ExitOnError)
	addClientFlags(flags, &opts)
	limit := flags.Int("limit", 100, "reply limit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	var result struct {
		Root        store.Message     `json:"root"`
		Replies     []store.Message   `json:"replies"`
		ThreadState store.ThreadState `json:"thread_state"`
	}
	path := fmt.Sprintf("/api/messages/%s/thread?limit=%d", url.PathEscape(messageID), *limit)
	if err := c.get(path, &result); err != nil {
		return err
	}
	if c.opts.JSON {
		return writeJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stdout, "%s\t%s\n", result.Root.ID, result.Root.Body)
	for _, reply := range result.Replies {
		author := reply.AuthorID
		if reply.Author != nil {
			author = reply.Author.DisplayName
		}
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", reply.ID, author, reply.Body)
	}
	return nil
}

func (c apiClient) threadReply(messageID string, args []string) error {
	opts := c.opts
	bodyFromFlag, stdin, file, replyTo, rest, err := parseBodyCommand("threads reply", args, &opts)
	if err != nil {
		return err
	}
	c = c.withOptions(opts, true)
	body, err := resolveBody(*bodyFromFlag, *file, *stdin, rest)
	if err != nil {
		return err
	}
	var result struct {
		Message     store.Message     `json:"message"`
		ThreadState store.ThreadState `json:"thread_state"`
		Events      []store.Event     `json:"events"`
	}
	payload := messagePayload(body, *replyTo)
	if err := c.doJSON(context.Background(), http.MethodPost, "/api/messages/"+url.PathEscape(messageID)+"/thread/replies", payload, &result); err != nil {
		return err
	}
	return c.write(result, result.Message.ID, fmt.Sprintf("replied %s to %s\n", result.Message.ID, messageID))
}

func parseBodyCommand(name string, args []string, opts *clientOptions) (*string, *bool, *string, *string, []string, error) {
	flags := flag.NewFlagSet(name, flag.ExitOnError)
	addClientFlags(flags, opts)
	body := flags.String("body", "", "message body")
	stdin := flags.Bool("stdin", false, "read message body from stdin")
	file := flags.String("file", "", "read message body from file")
	replyTo := flags.String("reply-to", "", "quote a message ID (must be in the same channel, conversation, or thread)")
	if err := flags.Parse(args); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return body, stdin, file, replyTo, flags.Args(), nil
}

func messagePayload(body, replyTo string) map[string]string {
	payload := map[string]string{"body": body}
	if trimmed := strings.TrimSpace(replyTo); trimmed != "" {
		payload["quoted_message_id"] = trimmed
	}
	return payload
}

func resolveBody(bodyFlag, file string, stdin bool, args []string) (string, error) {
	switch {
	case strings.TrimSpace(bodyFlag) != "":
		return bodyFlag, nil
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case stdin:
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case len(args) > 0:
		return strings.Join(args, " "), nil
	default:
		return "", errors.New("message body is required")
	}
}

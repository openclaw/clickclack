package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

const maxBotCommands = 100

var botCommandPattern = regexp.MustCompile(`^/[a-z0-9_-]{1,32}$`)

func (s *Store) SetBotCommands(ctx context.Context, workspaceID, botUserID string, commands []store.BotCommandInput) ([]store.BotCommand, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, errors.New("workspace_id is required")
	}
	botUserID = strings.TrimSpace(botUserID)
	if botUserID == "" {
		return nil, errors.New("bot_user_id is required")
	}
	normalized, err := normalizeBotCommands(commands)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.LockBotCommandSet(ctx, storedb.LockBotCommandSetParams{
		WorkspaceID: workspaceID,
		BotUserID:   botUserID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrModerationRestricted
		}
		return nil, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, workspaceID, botUserID); err != nil {
		return nil, err
	}
	if err := qtx.DeleteBotCommandsForBot(ctx, storedb.DeleteBotCommandsForBotParams{
		WorkspaceID: workspaceID,
		BotUserID:   botUserID,
	}); err != nil {
		return nil, err
	}
	timestamp := now()
	for _, command := range normalized {
		if err := qtx.InsertBotCommand(ctx, storedb.InsertBotCommandParams{
			ID:          newID("botcmd"),
			WorkspaceID: workspaceID,
			BotUserID:   botUserID,
			Command:     command.Command,
			Description: command.Description,
			ArgsHint:    command.ArgsHint,
			CreatedAt:   timestamp,
			UpdatedAt:   timestamp,
		}); err != nil {
			return nil, err
		}
	}
	rows, err := qtx.ListBotCommandsForBot(ctx, storedb.ListBotCommandsForBotParams{
		WorkspaceID: workspaceID,
		BotUserID:   botUserID,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	out := make([]store.BotCommand, 0, len(rows))
	for _, row := range rows {
		out = append(out, storeBotCommand(row))
	}
	return out, nil
}

func (s *Store) ListBotCommands(ctx context.Context, workspaceID, requesterID string) ([]store.WorkspaceBotCommand, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, errors.New("workspace_id is required")
	}
	requesterID = strings.TrimSpace(requesterID)
	if requesterID == "" {
		return nil, errors.New("requester_id is required")
	}
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListWorkspaceBotCommands(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]store.WorkspaceBotCommand, 0, len(rows))
	for _, row := range rows {
		out = append(out, store.WorkspaceBotCommand{
			ID:          row.ID,
			Command:     row.Command,
			Description: row.Description,
			ArgsHint:    row.ArgsHint,
			Bot: store.BotCommandBot{
				ID:          row.BotUserID,
				Handle:      row.BotHandle,
				DisplayName: row.BotDisplayName,
				AvatarURL:   row.BotAvatarUrl,
			},
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}

func normalizeBotCommands(commands []store.BotCommandInput) ([]store.BotCommandInput, error) {
	if len(commands) > maxBotCommands {
		return nil, fmt.Errorf("commands must contain at most %d entries", maxBotCommands)
	}
	normalized := make([]store.BotCommandInput, 0, len(commands))
	seen := make(map[string]struct{}, len(commands))
	for i, input := range commands {
		command := normalizeSlashCommand(input.Command)
		if !botCommandPattern.MatchString(command) {
			return nil, fmt.Errorf("commands[%d].command must match %s after normalization", i, botCommandPattern)
		}
		description := strings.TrimSpace(input.Description)
		if description == "" {
			return nil, fmt.Errorf("commands[%d].description is required", i)
		}
		if utf8.RuneCountInString(description) > 100 {
			return nil, fmt.Errorf("commands[%d].description must be at most 100 characters", i)
		}
		argsHint := strings.TrimSpace(input.ArgsHint)
		if utf8.RuneCountInString(argsHint) > 100 {
			return nil, fmt.Errorf("commands[%d].args_hint must be at most 100 characters", i)
		}
		if _, exists := seen[command]; exists {
			return nil, fmt.Errorf("commands[%d].command duplicates %s", i, command)
		}
		seen[command] = struct{}{}
		normalized = append(normalized, store.BotCommandInput{
			Command:     command,
			Description: description,
			ArgsHint:    argsHint,
		})
	}
	return normalized, nil
}

func storeBotCommand(row storedb.BotCommand) store.BotCommand {
	return store.BotCommand{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		BotUserID:   row.BotUserID,
		Command:     row.Command,
		Description: row.Description,
		ArgsHint:    row.ArgsHint,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

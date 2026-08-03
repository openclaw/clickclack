package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Store) ListTopics(ctx context.Context, workspaceID, requesterID string) ([]store.Topic, error) {
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	channels, err := s.ListChannels(ctx, workspaceID, requesterID)
	if err != nil {
		return nil, err
	}
	visibleChannels := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		visibleChannels[channel.ID] = struct{}{}
	}
	rows, err := s.db.QueryContext(ctx, topicSelect()+`
		WHERE workspace_id = $1 AND archived_at IS NULL
		ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	topics, err := scanTopics(rows)
	if err != nil {
		return nil, err
	}
	visible := topics[:0]
	for _, topic := range topics {
		if topic.ChannelID == "" {
			visible = append(visible, topic)
			continue
		}
		if _, ok := visibleChannels[topic.ChannelID]; ok {
			visible = append(visible, topic)
		}
	}
	return visible, nil
}

func (s *Store) CreateTopic(ctx context.Context, input store.CreateTopicInput) (store.Topic, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	createdBy := strings.TrimSpace(input.CreatedBy)
	name := strings.TrimSpace(input.Name)
	if workspaceID == "" || name == "" {
		return store.Topic{}, errors.New("topic name is required")
	}
	if len(name) > 80 {
		return store.Topic{}, errors.New("topic name is too long")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Topic{}, err
	}
	defer tx.Rollback()
	if err := requireMembershipTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.Topic{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.Topic{}, err
	}
	channelID := strings.TrimSpace(input.ChannelID)
	if channelID != "" {
		var channelWorkspace string
		if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM channels WHERE id = $1`, channelID).Scan(&channelWorkspace); err != nil {
			return store.Topic{}, err
		}
		if channelWorkspace != workspaceID {
			return store.Topic{}, errors.New("topic channel is not in workspace")
		}
		if err := requireCanPostTx(ctx, tx, workspaceID, channelID, createdBy); err != nil {
			return store.Topic{}, err
		}
	} else if err := requireNonGuestTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.Topic{}, err
	}
	topic := store.Topic{
		ID:          newID("top"),
		WorkspaceID: workspaceID,
		ChannelID:   channelID,
		Name:        name,
		CreatedBy:   createdBy,
		CreatedAt:   now(),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO topics (id, workspace_id, channel_id, name, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		topic.ID,
		topic.WorkspaceID,
		sqlOptionalText(topic.ChannelID),
		topic.Name,
		sqlOptionalText(topic.CreatedBy),
		topic.CreatedAt,
	)
	if err != nil {
		return store.Topic{}, err
	}
	return topic, tx.Commit()
}

func requireTopicTx(ctx context.Context, tx *sql.Tx, workspaceID, channelID, topicID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil
	}
	var topicWorkspace, topicChannel string
	if err := tx.QueryRowContext(ctx, `SELECT workspace_id, COALESCE(channel_id, '') FROM topics WHERE id = $1 AND archived_at IS NULL`, topicID).Scan(&topicWorkspace, &topicChannel); err != nil {
		return err
	}
	if topicWorkspace != workspaceID {
		return errors.New("topic is not in workspace")
	}
	if topicChannel != "" && topicChannel != channelID {
		return errors.New("topic is not available in this channel")
	}
	return nil
}

func (s *Store) requireTopic(ctx context.Context, workspaceID, channelID, topicID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil
	}
	topic, err := s.q.GetAvailableTopic(ctx, topicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: topic is unavailable", store.ErrInvalidMessagePage)
		}
		return err
	}
	if topic.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: topic is unavailable", store.ErrInvalidMessagePage)
	}
	if topic.ChannelID != "" && topic.ChannelID != channelID {
		return fmt.Errorf("%w: topic is unavailable", store.ErrInvalidMessagePage)
	}
	return nil
}

func topicSelect() string {
	return `SELECT id, workspace_id, COALESCE(channel_id, ''), name, created_by, created_at, archived_at FROM topics`
}

func scanTopics(rows *sql.Rows) ([]store.Topic, error) {
	out := []store.Topic{}
	for rows.Next() {
		var topic store.Topic
		var createdBy, archivedAt sql.NullString
		if err := rows.Scan(&topic.ID, &topic.WorkspaceID, &topic.ChannelID, &topic.Name, &createdBy, &topic.CreatedAt, &archivedAt); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			topic.CreatedBy = createdBy.String
		}
		if archivedAt.Valid {
			topic.ArchivedAt = &archivedAt.String
		}
		out = append(out, topic)
	}
	return out, rows.Err()
}

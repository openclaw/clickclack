package httpapi

import (
	"context"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Server) CleanupPendingUploadObjects(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = uploadCleanupSweepLimit
	}
	attempted := make(map[string]struct{})
	var cleanupErrors []error
	queryLimit := limit
	for {
		cleanups, err := s.store.ListPendingUploadCleanups(ctx, queryLimit)
		if err != nil {
			return err
		}
		if len(cleanups) == 0 {
			return errors.Join(cleanupErrors...)
		}
		pending := cleanups[:0]
		for _, cleanup := range cleanups {
			if _, seen := attempted[cleanup.ID]; seen {
				continue
			}
			attempted[cleanup.ID] = struct{}{}
			pending = append(pending, cleanup)
		}
		if len(pending) == 0 {
			if len(cleanups) < queryLimit {
				return errors.Join(cleanupErrors...)
			}
			queryLimit += limit
			continue
		}
		if err := s.cleanupUploadObjects(ctx, pending); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
}

func (s *Server) cleanupUploadObjects(ctx context.Context, cleanups []store.PendingUploadCleanup) error {
	if len(cleanups) == 0 {
		return nil
	}
	if s.uploadStorage == nil {
		err := errors.New("upload storage is not configured")
		for _, cleanup := range cleanups {
			_ = s.store.RecordPendingUploadCleanupFailure(ctx, cleanup.ID, err.Error())
		}
		return err
	}
	var cleanupErrors []error
	for _, cleanup := range cleanups {
		if err := s.uploadStorage.Delete(ctx, cleanup.StoragePath); err != nil {
			_ = s.store.RecordPendingUploadCleanupFailure(ctx, cleanup.ID, err.Error())
			cleanupErrors = append(cleanupErrors, err)
			continue
		}
		if err := s.store.DeletePendingUploadCleanup(ctx, cleanup.ID); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

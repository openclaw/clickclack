package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) GetAppearancePreferences(ctx context.Context, userID string) (*store.AppearancePreferences, error) {
	row, err := s.q.GetAppearancePreferences(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	preferences := store.AppearancePreferences{
		ColorMode:     row.ColorMode,
		BoardTheme:    row.BoardTheme,
		MessageLayout: row.MessageLayout,
		Density:       row.Density,
	}
	return &preferences, nil
}

func updateAppearancePreferences(ctx context.Context, q *storedb.Queries, userID string, patch store.AppearancePreferencesPatch) error {
	if err := q.EnsureAppearancePreferences(ctx, userID); err != nil {
		return err
	}
	if patch.ColorMode != nil {
		if err := q.UpdateAppearanceColorMode(ctx, storedb.UpdateAppearanceColorModeParams{
			ColorMode: *patch.ColorMode,
			UserID:    userID,
		}); err != nil {
			return err
		}
	}
	if patch.BoardTheme != nil {
		if err := q.UpdateAppearanceBoardTheme(ctx, storedb.UpdateAppearanceBoardThemeParams{
			BoardTheme: *patch.BoardTheme,
			UserID:     userID,
		}); err != nil {
			return err
		}
	}
	if patch.MessageLayout != nil {
		if err := q.UpdateAppearanceMessageLayout(ctx, storedb.UpdateAppearanceMessageLayoutParams{
			MessageLayout: *patch.MessageLayout,
			UserID:        userID,
		}); err != nil {
			return err
		}
	}
	if patch.Density != nil {
		if err := q.UpdateAppearanceDensity(ctx, storedb.UpdateAppearanceDensityParams{
			Density: *patch.Density,
			UserID:  userID,
		}); err != nil {
			return err
		}
	}
	return nil
}

package storetest

import (
	"context"
	"errors"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// CreateUpload builds fixtures through the same reservation lifecycle as HTTP uploads.
func CreateUpload(ctx context.Context, st store.Store, input store.CreateUploadInput) (store.Upload, error) {
	reservation, err := st.ReserveUploadQuota(ctx, input.WorkspaceID, input.OwnerID, input.Nonce, input.ByteSize)
	if err != nil {
		return store.Upload{}, err
	}
	upload, err := st.CreateReservedUpload(ctx, reservation.ID, input)
	if err != nil {
		err = errors.Join(err, st.ReleaseUploadQuotaReservation(ctx, reservation.ID, input.OwnerID))
	}
	return upload, err
}

package store

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
)

// DirectConversationMemberSetKey returns a stable key for canonical one-to-one
// conversations. Group conversations intentionally remain non-canonical.
func DirectConversationMemberSetKey(memberIDs []string) string {
	if len(memberIDs) != 2 {
		return ""
	}
	ids := append([]string(nil), memberIDs...)
	sort.Strings(ids)
	hash := sha256.New()
	var size [8]byte
	for _, id := range ids {
		binary.BigEndian.PutUint64(size[:], uint64(len(id)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(id))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

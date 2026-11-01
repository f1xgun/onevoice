package storage

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOwnedMediaExtension(t *testing.T) {
	businessID := uuid.New()
	prefix := "businesses/" + businessID.String() + "/media/"
	id := uuid.NewString()
	for _, origin := range []string{"/media", "https://uploads.example/bucket"} {
		store := newTestClient(origin)
		for _, ext := range []string{".jpg", ".png", ".webp"} {
			got, err := OwnedMediaExtension(store, businessID, store.PublicURL(prefix+id+ext))
			require.NoError(t, err)
			require.Equal(t, ext, got)
		}
		for _, key := range []string{
			"businesses/" + uuid.NewString() + "/media/" + id + ".png",
			prefix + "../" + id + ".png", prefix + "%2e%2e.png", prefix + "not-a-uuid.png",
			prefix + id + ".gif", prefix + id + ".png?x=1", prefix + id + ".png#fragment",
			prefix + id + "/file.png", prefix + "urn:uuid:" + id + ".png",
		} {
			_, err := OwnedMediaExtension(store, businessID, store.PublicURL(key))
			require.Error(t, err, key)
		}
		_, err := OwnedMediaExtension(store, businessID, "https://foreign.example/"+prefix+id+".png")
		require.Error(t, err)
	}
	_, err := OwnedMediaExtension(nil, businessID, "/media/"+prefix+id+".png")
	require.Error(t, err)
}

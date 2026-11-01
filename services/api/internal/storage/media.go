package storage

import (
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
)

// OwnedMediaExtension checks the canonical upload URL and tenant media key.
// It never fetches the URL or accepts a provider-generated image URL.
func OwnedMediaExtension(store Uploader, businessID uuid.UUID, url string) (string, error) {
	if store == nil {
		return "", fmt.Errorf("upload store unavailable")
	}
	key := store.KeyFromPublicURL(url)
	prefix := "businesses/" + businessID.String() + "/media/"
	if !strings.HasPrefix(key, prefix) || url != store.PublicURL(key) {
		return "", fmt.Errorf("image outside business media")
	}
	filename := strings.TrimPrefix(key, prefix)
	ext := path.Ext(filename)
	if strings.ContainsAny(filename, "/\\%?#") || (ext != ".jpg" && ext != ".png" && ext != ".webp") {
		return "", fmt.Errorf("invalid media key")
	}
	id, err := uuid.Parse(strings.TrimSuffix(filename, ext))
	if err != nil || filename != id.String()+ext {
		return "", fmt.Errorf("invalid media key")
	}
	return ext, nil
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/pkg/natsauth"
)

func TestRenderUserReplyPermissions(t *testing.T) {
	for _, permission := range perms {
		t.Run(permission.service, func(t *testing.T) {
			rendered := renderUser(permission, "public-key")
			hasReply := strings.Contains(rendered, "allow_responses:")
			if hasReply != permission.allowResponses {
				t.Fatal("incorrect reply permission")
			}
			if hasReply && !strings.Contains(rendered, `allow_responses: { max: 1, expires: "`+natsauth.ResponsePermissionTTL.String()+`" }`) {
				t.Fatal("reply must be limited to one response within the shared window")
			}
			if strings.Contains(rendered, `publish { allow: ["_INBOX.>"] }`) {
				t.Fatal("unrestricted inbox publishing")
			}
		})
	}
}

func TestServiceKeyRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.nk")
	key, err := serviceKey(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Wipe()
	seed, err := key.Seed()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(seed)
	if err := os.WriteFile(path, seed, seedFileMode); err != nil {
		t.Fatal(err)
	}
	refreshed, err := serviceKey(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer refreshed.Wipe()
	originalPublic, err := key.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	refreshedPublic, err := refreshed.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if originalPublic != refreshedPublic {
		t.Fatal("refresh rotated the identity")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != seedFileMode {
		t.Fatal("seed permissions changed")
	}
}

func TestServiceKeyRefreshRejectsMissingOrInvalidSeed(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "invalid"}[exists], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "service.nk")
			if exists {
				if err := os.WriteFile(path, []byte("invalid"), seedFileMode); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := serviceKey(path, true); err == nil {
				t.Fatal("refresh must not replace an invalid or missing identity")
			}
		})
	}
}

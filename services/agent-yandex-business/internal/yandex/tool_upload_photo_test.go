package yandex

import (
	"os"
	"sync"
	"testing"
)

// TestStageTempPhoto_UniquePaths proves staging two photos never collides, even
// when invoked back-to-back within the same millisecond. The previous
// fmt.Sprintf("/tmp/upload_%d.jpg", UnixMilli) path is shared across concurrent
// business contexts, so one upload could clobber another tenant's bytes.
func TestStageTempPhoto_UniquePaths(t *testing.T) {
	const n = 50
	seen := make(map[string]struct{}, n)
	var paths []string
	for i := 0; i < n; i++ {
		p, err := stageTempPhoto([]byte("img"))
		if err != nil {
			t.Fatalf("stageTempPhoto: %v", err)
		}
		paths = append(paths, p)
		if _, dup := seen[p]; dup {
			t.Fatalf("stageTempPhoto returned duplicate path %q on call %d", p, i)
		}
		seen[p] = struct{}{}
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// TestStageTempPhoto_ConcurrentNoCollision stresses the path under genuine
// concurrency (the BrowserPool runs multiple business contexts in parallel).
// Each goroutine must get its own file with its own bytes intact.
func TestStageTempPhoto_ConcurrentNoCollision(t *testing.T) {
	const n = 64
	var (
		mu    sync.Mutex
		paths = make(map[string]struct{}, n)
		wg    sync.WaitGroup
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		payload := []byte{byte(i)}
		go func() {
			defer wg.Done()
			p, err := stageTempPhoto(payload)
			if err != nil {
				t.Errorf("stageTempPhoto: %v", err)
				return
			}
			defer func() { _ = os.Remove(p) }()
			got, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("read staged file: %v", err)
				return
			}
			if len(got) != 1 || got[0] != payload[0] {
				t.Errorf("staged file %q = %v, want %v (cross-tenant clobber)", p, got, payload)
			}
			mu.Lock()
			defer mu.Unlock()
			if _, dup := paths[p]; dup {
				t.Errorf("duplicate temp path %q across concurrent uploads", p)
			}
			paths[p] = struct{}{}
		}()
	}
	wg.Wait()
}

// TestStageTempPhoto_FileMode confirms the staged file keeps the owner-only
// permission used for media staging.
func TestStageTempPhoto_FileMode(t *testing.T) {
	p, err := stageTempPhoto([]byte("img"))
	if err != nil {
		t.Fatalf("stageTempPhoto: %v", err)
	}
	defer func() { _ = os.Remove(p) }()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	if info.Mode().Perm() != tmpFileMode {
		t.Fatalf("staged file mode = %o, want %o", info.Mode().Perm(), tmpFileMode)
	}
}

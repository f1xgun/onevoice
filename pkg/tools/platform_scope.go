package tools

import "fmt"

const MaxSelectedPlatforms = 3

var selectablePlatformIDs = map[string]struct{}{
	"telegram":        {},
	"vk":              {},
	"yandex_business": {},
}

// NormalizeSelectedPlatforms validates, deduplicates, and preserves the order
// of an explicitly supplied guided-compose platform scope.
func NormalizeSelectedPlatforms(platforms []string) ([]string, error) {
	if len(platforms) == 0 {
		return nil, fmt.Errorf("selected_platforms must contain at least one platform")
	}
	if len(platforms) > MaxSelectedPlatforms {
		return nil, fmt.Errorf("selected_platforms must contain at most %d platforms", MaxSelectedPlatforms)
	}
	result := make([]string, 0, len(platforms))
	seen := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		if _, ok := selectablePlatformIDs[platform]; !ok {
			return nil, fmt.Errorf("unsupported selected platform %q", platform)
		}
		if _, duplicate := seen[platform]; duplicate {
			continue
		}
		seen[platform] = struct{}{}
		result = append(result, platform)
	}
	return result, nil
}

// IntersectSelectedPlatforms applies a validated explicit scope to the fresh
// active integration list. A non-nil empty result remains scoped and closed.
func IntersectSelectedPlatforms(active, selected []string) []string {
	allowed := make(map[string]struct{}, len(selected))
	for _, platform := range selected {
		allowed[platform] = struct{}{}
	}
	result := make([]string, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, platform := range active {
		if _, ok := allowed[platform]; !ok {
			continue
		}
		if _, duplicate := seen[platform]; duplicate {
			continue
		}
		seen[platform] = struct{}{}
		result = append(result, platform)
	}
	return result
}

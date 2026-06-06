package prompt_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
)

// blockOneRuSHA256 is the locked sha256 of the Russian Block 1 (platform-wide
// system prompt prefix). Block 1 is padded to ~1957 estimated tokens to clear
// Anthropic Sonnet 4.6's 1024-token cache minimum (without padding the cache
// would not engage and the prompt-cache savings would not materialize). If
// this hash changes, Anthropic's prompt-cache prefix INVALIDATES on the next
// deploy and every business pays full input cost on its first turn — so the
// change MUST be deliberate. To rotate the hash: re-run
// TestSystemPromptHash_Stability_LockedHash with -v, copy the printed sha256
// into this constant, and document the prompt change in the PR description.
const blockOneRuSHA256 = "8b9b193fcc0dd9addcfbd6fb136bdb4dcda11592a3b6b7e7819626ecff379acf"

// blockOneEnSHA256 is the locked sha256 of the English Block 1 (padded to
// ~1189 estimated tokens, safely above Sonnet 4.6's 1024-token cache floor).
// See blockOneRuSHA256 doc comment for rotation procedure.
const blockOneEnSHA256 = "55cec729cbf2888143b4e14c32dcab38facad9b8a95af93a16accb4b923ce9bd"

// TestSystemPromptHash_Stability proves Block 1 (platform-wide) is BYTE-
// IDENTICAL across BusinessContext variations for the same locale — this is
// the regression guard for Anthropic's cross-business prompt-cache prefix.
// If any business-state field leaks into Block 1, this test fails and the
// cache prefix churns per business, defeating the cache.
func TestSystemPromptHash_Stability(t *testing.T) {
	tag := language.Russian
	a, _, _ := prompt.BuildSplit(prompt.BusinessContext{
		Name:   "A",
		Locale: tag,
		Now:    time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
	}, nil, nil)
	b, _, _ := prompt.BuildSplit(prompt.BusinessContext{
		Name:               "Совсем другой бизнес",
		Category:           "кафе",
		Address:            "Москва",
		ActiveIntegrations: []string{"telegram", "vk"},
		Locale:             tag,
		Now:                time.Date(2026, 7, 4, 18, 30, 0, 0, time.UTC),
	}, nil, nil)

	hashA := sha256.Sum256([]byte(a))
	hashB := sha256.Sum256([]byte(b))
	assert.Equal(t, hashA, hashB,
		"Block 1 (platform) MUST be byte-identical across BusinessContext variations for cache stability — got A=%q B=%q",
		a, b,
	)
}

// TestSystemPromptHash_Stability_LockedHash pins the Block 1 RU hash to a
// constant value. To rotate (intentional platform-prompt change): run with
// `-v`, copy the failing-actual sha256 into blockOneRuSHA256, commit.
func TestSystemPromptHash_Stability_LockedHash(t *testing.T) {
	t.Run("ru", func(t *testing.T) {
		platform, _, _ := prompt.BuildSplit(prompt.BusinessContext{
			Locale: language.Russian,
			Now:    time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
		}, nil, nil)
		sum := sha256.Sum256([]byte(platform))
		got := hex.EncodeToString(sum[:])
		assert.Equal(t, blockOneRuSHA256, got,
			"Block 1 (RU) sha256 drifted. Either (a) intentional platform change — rotate blockOneRuSHA256 to %s, or (b) regression — investigate.\nactual block 1:\n%s",
			got, platform,
		)
	})
	t.Run("en", func(t *testing.T) {
		platform, _, _ := prompt.BuildSplit(prompt.BusinessContext{
			Locale: language.English,
			Now:    time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
		}, nil, nil)
		sum := sha256.Sum256([]byte(platform))
		got := hex.EncodeToString(sum[:])
		assert.Equal(t, blockOneEnSHA256, got,
			"Block 1 (EN) sha256 drifted. Rotate blockOneEnSHA256 to %s if intentional.\nactual block 1:\n%s",
			got, platform,
		)
	})
}

// TestSystemPromptHash_LocaleDifferentiated asserts RU vs EN Block 1 are
// DISTINCT — the language directive ("Общайся на русском языке" /
// "Respond in English") is part of Block 1, so different locales must produce
// different hashes (each individually stable, see TestSystemPromptHash_Stability).
func TestSystemPromptHash_LocaleDifferentiated(t *testing.T) {
	ru, _, _ := prompt.BuildSplit(prompt.BusinessContext{Locale: language.Russian}, nil, nil)
	en, _, _ := prompt.BuildSplit(prompt.BusinessContext{Locale: language.English}, nil, nil)
	hashRu := sha256.Sum256([]byte(ru))
	hashEn := sha256.Sum256([]byte(en))
	assert.NotEqual(t, hashRu, hashEn,
		"RU vs EN Block 1 hashes must differ — language directive is load-bearing in Block 1",
	)
}

// TestBuildSplit_BlockOrdering pins the structural invariants of the two-block
// split. Block 1 MUST contain the rules + language directive and MUST NOT
// contain business state; Block 2 MUST carry the business state.
func TestBuildSplit_BlockOrdering(t *testing.T) {
	t.Run("ru", func(t *testing.T) {
		platform, business, _ := prompt.BuildSplit(prompt.BusinessContext{
			Name:               "Кофейня Уют",
			Category:           "кофейня",
			Locale:             language.Russian,
			ActiveIntegrations: []string{"telegram"},
			Now:                time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
		}, nil, nil)

		assert.True(t, strings.HasPrefix(platform, "Ты — AI-ассистент"),
			"Block 1 (RU) must start with the preamble: %q", platform)
		assert.Contains(t, platform, "## Правила", "Block 1 must contain the rules header")
		assert.Contains(t, platform, "Общайся на русском языке",
			"Block 1 must end with the load-bearing language directive")
		assert.NotContains(t, platform, "## Бизнес",
			"Block 1 must NOT contain business state — regression guard for cache stability")
		assert.NotContains(t, platform, "Кофейня Уют",
			"Block 1 must NOT interpolate business name")
		assert.NotContains(t, platform, "Текущая дата и время",
			"Block 1 must NOT carry per-turn timestamp")

		assert.Contains(t, business, "## Бизнес: Кофейня Уют",
			"Block 2 must carry business name header")
		assert.Contains(t, business, "Текущая дата и время:",
			"Block 2 must carry the per-turn timestamp")
		assert.Contains(t, business, "## Активные интеграции",
			"Block 2 must carry active integrations list")
	})

	t.Run("en", func(t *testing.T) {
		platform, business, _ := prompt.BuildSplit(prompt.BusinessContext{
			Name:               "Cozy Cafe",
			Category:           "cafe",
			Locale:             language.English,
			ActiveIntegrations: []string{"telegram"},
			Now:                time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
		}, nil, nil)

		assert.True(t, strings.HasPrefix(platform, "You are an AI assistant"),
			"Block 1 (EN) must start with the English preamble: %q", platform)
		assert.Contains(t, platform, "## Rules")
		assert.Contains(t, platform, "Respond in English")
		assert.NotContains(t, platform, "## Business",
			"Block 1 (EN) must NOT contain business state")
		assert.NotContains(t, platform, "Cozy Cafe")

		assert.Contains(t, business, "## Business: Cozy Cafe")
		assert.Contains(t, business, "Current date and time:")
		assert.Contains(t, business, "## Active integrations")
	})
}

// TestBuildSplit_HistoryPassthrough asserts BuildSplit returns the history
// slice verbatim — it does NOT prepend a system message (caller must wire
// SystemBlocks separately).
func TestBuildSplit_HistoryPassthrough(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
	}
	_, _, msgs := prompt.BuildSplit(prompt.BusinessContext{Locale: language.Russian}, nil, history)
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "first", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.NotEqual(t, "system", msgs[0].Role, "BuildSplit must NOT prepend a role:system message")
}

// TestBuildSplit_ZeroBusinessContext asserts the zero-value BusinessContext
// renders a valid (non-empty) Block 1 and a Block 2 that emits only the
// always-present sections (header + default tone + Now + no-integrations).
func TestBuildSplit_ZeroBusinessContext(t *testing.T) {
	platform, business, _ := prompt.BuildSplit(prompt.BusinessContext{}, nil, nil)
	assert.NotEmpty(t, platform, "Block 1 must be non-empty even for zero BusinessContext")
	assert.NotEmpty(t, business, "Block 2 must be non-empty even for zero BusinessContext")
	assert.NotContains(t, platform, "Категория:", "zero context must not emit Категория in Block 1")
	assert.Contains(t, business, "## Бизнес:", "Block 2 still carries the header even with empty Name")
}

// TestBuildSplit_ProjectAttachesToBusinessBlock asserts ProjectContext is
// appended to Block 2 (per-business / per-conversation layer) so Block 1
// stays platform-pure and cache-stable.
func TestBuildSplit_ProjectAttachesToBusinessBlock(t *testing.T) {
	proj := &prompt.ProjectContext{
		ID:           "p1",
		Name:         "Отзывы",
		SystemPrompt: "Отвечай вежливо",
	}
	platform, business, _ := prompt.BuildSplit(prompt.BusinessContext{
		Name:   "Кофейня",
		Locale: language.Russian,
		Now:    time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
	}, proj, nil)

	assert.NotContains(t, platform, "## Проект",
		"Project block must NOT contaminate Block 1 (platform cache stability)")
	assert.Contains(t, business, "## Проект: Отзывы",
		"Project block must attach to Block 2 (per-business layer)")
	assert.Contains(t, business, "Отвечай вежливо",
		"Project system prompt must be inside Block 2")
}

package service

import "testing"

func TestRuneLevenshtein(t *testing.T) {
	cases := []struct {
		name, a, b string
		want       int
	}{
		{"identical", "hello", "hello", 0},
		{"empty both", "", "", 0},
		{"empty a", "", "abc", 3},
		{"empty b", "abc", "", 3},
		{"one substitution", "cat", "car", 1},
		{"one insertion", "cat", "cart", 1},
		{"cyrillic identical", "Спасибо за отзыв!", "Спасибо за отзыв!", 0},
		{"cyrillic one char", "Спасибо", "Спосибо", 1},
		// Byte length differs from rune length for Cyrillic; the distance must be
		// counted per rune, so appending one Cyrillic char is distance 1, not 2.
		{"cyrillic append one rune", "Спасибо", "Спасибо!", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runeLevenshtein(c.a, c.b); got != c.want {
				t.Fatalf("runeLevenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
			}
			if got := runeLevenshtein(c.b, c.a); got != c.want {
				t.Fatalf("distance must be symmetric: (%q,%q) = %d, want %d", c.b, c.a, got, c.want)
			}
		})
	}
}

func TestDraftReplyFeedback(t *testing.T) {
	t.Run("no draft yields no signal", func(t *testing.T) {
		if fb := draftReplyFeedback("", "some manual reply"); fb != nil {
			t.Fatalf("a reply with no prior draft must carry no signal, got %+v", fb)
		}
		if fb := draftReplyFeedback("   ", "reply"); fb != nil {
			t.Fatalf("a blank draft must carry no signal, got %+v", fb)
		}
		if fb := draftReplyFeedback("real draft", "   "); fb != nil {
			t.Fatalf("a whitespace-only final reply must carry no signal, got %+v", fb)
		}
	})

	t.Run("verbatim send is accepted with zero distance", func(t *testing.T) {
		fb := draftReplyFeedback("Спасибо за тёплый отзыв!", "  Спасибо за тёплый отзыв!  ")
		if fb == nil || !fb.AcceptedUnedited || fb.EditDistance != 0 {
			t.Fatalf("a trimmed-verbatim send must be accepted at distance 0, got %+v", fb)
		}
	})

	t.Run("light copy-edit still counts as accepted", func(t *testing.T) {
		fb := draftReplyFeedback("Спасибо за ваш отзыв, ждём вас снова", "Спасибо за ваш отзыв, ждем вас снова!")
		if fb == nil || !fb.AcceptedUnedited {
			t.Fatalf("a light edit (ё→е + '!') must still count as accepted, got %+v", fb)
		}
		if fb.EditDistance == 0 {
			t.Fatalf("a light edit must record a non-zero distance")
		}
	})

	t.Run("substantial rewrite is not accepted", func(t *testing.T) {
		fb := draftReplyFeedback(
			"Спасибо за отзыв, приходите ещё!",
			"Нам очень жаль, что вам не понравилось. Мы обязательно разберёмся и всё исправим в ближайшее время.")
		if fb == nil || fb.AcceptedUnedited {
			t.Fatalf("a full rewrite must NOT count as accepted, got %+v", fb)
		}
		if fb.EditDistance == 0 {
			t.Fatalf("a rewrite must record a large distance")
		}
	})
}

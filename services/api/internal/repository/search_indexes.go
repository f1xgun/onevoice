package repository

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// EnsureSearchIndexes — v20.1: best-effort cleanup of legacy text
// indexes.
//
// History:
//
//   - v19 created `_text_v19` indexes with default_language "russian"
//     (Snowball). These had asymmetric stems (e.g. "отзыв" vs "отзывы")
//     that broke recall.
//   - v20 renamed to `_text_v20` with default_language "none" — fixed
//     the asymmetry but lost morphological recall.
//   - v20.1 abandons $text entirely. Search uses word-prefix regex
//     (see message.go::SearchByConversationIDs and
//     conversation.go::SearchTitles), which works against the existing
//     scope indexes (`conversation_id_1_created_at_1` on messages,
//     ESR pin-aware compound on conversations).
//
// This function now exists ONLY to drop the legacy text indexes left
// behind by previous deploys. Drops are best-effort — IndexNotFound
// (code 27) is the steady-state no-op.
//
// We keep the function (rather than removing it) so cmd/main.go's
// startup wiring stays unchanged; the readiness flag still flips after
// this returns and gates 503/Retry-After until the search service is
// wired.
func EnsureSearchIndexes(ctx context.Context, db *mongo.Database) error {
	convs := db.Collection("conversations")
	msgs := db.Collection("messages")

	dropLegacy := func(coll *mongo.Collection, name string) {
		if err := coll.Indexes().DropOne(ctx, name); err != nil {
			var cmdErr mongo.CommandError
			if errors.As(err, &cmdErr) && cmdErr.Code == 27 {
				return
			}
			// Non-fatal: search itself doesn't need these indexes any more.
			_ = err
		}
	}
	dropLegacy(convs, "conversations_title_text_v19")
	dropLegacy(convs, "conversations_title_text_v20")
	dropLegacy(msgs, "messages_content_text_v19")
	dropLegacy(msgs, "messages_content_text_v20")
	return nil
}

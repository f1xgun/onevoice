package repository

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// EnsureSearchIndexes drops the legacy `_text_v19` / `_text_v20` text indexes
// left behind by previous deploys. Search uses word-prefix regex (see
// message.go::SearchByConversationIDs and conversation.go::SearchTitles)
// instead of $text. Drops are best-effort — IndexNotFound (code 27) is the
// steady-state no-op.
//
// The function is kept (rather than removed) so the startup wiring stays
// unchanged and the readiness flag still flips after this returns.
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

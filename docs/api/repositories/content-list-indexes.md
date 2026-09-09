# Content list indexes

Posts and reviews use descending `(created_at, _id)` ordering. API startup creates compound indexes with the organization scope first and both ordering fields last. Posts additionally have a status-prefixed index; reviews have platform- and reply-status-prefixed indexes. The ID suffix supports deterministic ordering when many records share a timestamp without a blocking sort.

These indexes have new names and coexist with the shorter indexes from `migrations/mongo/init.js` and earlier API releases. Startup creates them idempotently on existing installations as well as new databases. Creation errors stop startup instead of reporting successful initialization. Large existing collections may need the indexes provisioned before restarting the API if building them exceeds the startup timeout.

This change preserves the offset-based API contract. Deep offsets still scan preceding index entries, and exact total counts still examine the matching set. Cursor pagination remains a separate extension. Conversation recency still uses a computed fallback for legacy records and is not covered by these content indexes; message lists already have a conversation/time index.

`TestContentListIndexesAvoidBlockingSort` uses a unique disposable MongoDB database, installs legacy indexes, inserts two organizations with equal timestamps, and checks unhinted execution plans for the supported list filters at a deep offset. It requires an explicit `MONGODB_TEST_URI` and never selects a default database server.

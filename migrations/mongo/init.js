// MongoDB initialization script
// Run: mongosh mongodb://onevoice:onevoice_dev@localhost:27017/onevoice?authSource=admin < migrations/mongo/init.js

db = db.getSiblingDB('onevoice');

// Conversations collection indexes
db.conversations.createIndex({ "user_id": 1, "updated_at": -1 });

// Messages collection indexes
db.messages.createIndex({ "conversation_id": 1, "created_at": 1 });

// Tasks collection indexes
db.tasks.createIndex({ "business_id": 1, "created_at": -1 });
db.tasks.createIndex({ "status": 1 });

// Reviews collection indexes
db.reviews.createIndex({ "business_id": 1, "platform": 1, "created_at": -1 });
// external_id is per-business (e.g. VK builds "{post_id}_{comment_id}" from
// per-community sequential ints), so two organizations can legitimately share
// the same (external_id, platform). The uniqueness constraint must be scoped to
// business_id — matching the upsert natural key — or one org's review silently
// rejects with E11000 and is dropped.
db.reviews.createIndex(
  { "business_id": 1, "platform": 1, "external_id": 1 },
  { unique: true, name: "reviews_business_platform_external" },
);

// Posts collection indexes
db.posts.createIndex({ "business_id": 1, "created_at": -1 });
db.posts.createIndex({ "status": 1, "scheduled_at": 1 });

print("MongoDB indexes created successfully");

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
db.reviews.createIndex({ "external_id": 1, "platform": 1 }, { unique: true });

// Posts collection indexes
db.posts.createIndex({ "business_id": 1, "created_at": -1 });
db.posts.createIndex({ "status": 1, "scheduled_at": 1 });

print("MongoDB indexes created successfully");

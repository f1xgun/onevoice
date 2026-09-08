package repository

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// previewWhitespace matches Unicode whitespace plus the BOM accepted by the
// previous JavaScript preview normalizer.
const previewWhitespace = " \t\n\r\v\f\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

// conversationPreviewLookup returns only a bounded excerpt per paginated,
// authorized conversation. Message payloads and tool data never leave MongoDB.
func conversationPreviewLookup() bson.D {
	trimmed := bson.M{"$trim": bson.M{"input": bson.M{"$ifNull": bson.A{"$content", ""}}, "chars": previewWhitespace}}
	normalized := bson.M{"$reduce": bson.M{
		"input":        bson.M{"$regexFindAll": bson.M{"input": "$content", "regex": "[^" + previewWhitespace + "]+"}},
		"initialValue": "",
		"in": bson.M{"$concat": bson.A{
			"$$value",
			bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$$value", ""}}, "", " "}},
			"$$this.match",
		}},
	}}
	truncated := bson.M{"$rtrim": bson.M{
		"input": bson.M{"$substrCP": bson.A{"$$text", 0, 160}},
		"chars": previewWhitespace,
	}}
	return bson.D{{Key: "$lookup", Value: bson.M{
		"from":         "messages",
		"localField":   "_id",
		"foreignField": "conversation_id",
		"as":           "_preview",
		"pipeline": mongo.Pipeline{
			{{Key: "$match", Value: bson.M{
				"role":  bson.M{"$in": bson.A{"user", "assistant"}},
				"$expr": bson.M{"$ne": bson.A{trimmed, ""}},
			}}},
			{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}}},
			{{Key: "$limit", Value: 1}},
			{{Key: "$project", Value: bson.M{"_id": 0, "text": bson.M{"$let": bson.M{
				"vars": bson.M{"text": normalized},
				"in": bson.M{"$cond": bson.A{
					bson.M{"$gt": bson.A{bson.M{"$strLenCP": "$$text"}, 160}},
					bson.M{"$concat": bson.A{truncated, "…"}},
					"$$text",
				}},
			}}}}},
		},
	}}}
}

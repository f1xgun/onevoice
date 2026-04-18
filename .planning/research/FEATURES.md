# Feature Landscape: Google Business Profile API Integration

**Domain:** Google Business Profile management via API for OneVoice multi-agent platform
**Researched:** 2026-04-08
**Confidence:** HIGH (based on official Google developer documentation)

## API Landscape Overview

Google Business Profile is managed through a **federated set of APIs**, not a single unified endpoint. This is the most important architectural fact: different operations live on different API hosts and versions.

| API | Host | Version | Go Package | Status |
|-----|------|---------|------------|--------|
| Account Management | mybusinessaccountmanagement.googleapis.com | v1 | `mybusinessaccountmanagement/v1` | Active, maintenance mode |
| Business Information | mybusinessbusinessinformation.googleapis.com | v1 | `mybusinessbusinessinformation/v1` | Active, maintenance mode |
| Business Performance | businessprofileperformance.googleapis.com | v1 | `businessprofileperformance/v1` | Active, maintenance mode |
| Reviews (v4) | mybusiness.googleapis.com | v4 | **No Go client** -- direct HTTP | Active, no announced sunset |
| Local Posts (v4) | mybusiness.googleapis.com | v4 | **No Go client** -- direct HTTP | Active, no announced sunset |
| Media (v4) | mybusiness.googleapis.com | v4 | **No Go client** -- direct HTTP | Active, no announced sunset |
| Q&A | N/A | N/A | N/A | **Discontinued** Nov 3, 2025 |
| Place Actions | mybusinessplaceactions.googleapis.com | v1 | `mybusinessplaceactions/v1` | Active |
| Verifications | mybusinessverifications.googleapis.com | v1 | `mybusinessverifications/v1` | Active |

**Critical finding:** Reviews, Local Posts, and Media remain on the legacy v4 API with no announced migration path or sunset date. There are no auto-generated Go client libraries for these v4 endpoints. The agent must use direct HTTP REST calls with OAuth2 tokens for these operations.

## Table Stakes

Features users expect from a Google Business Profile management tool. Missing = the integration feels incomplete.

| Feature | Why Expected | Complexity | API | Dependencies |
|---------|--------------|------------|-----|--------------|
| **List reviews** | Core value of GBP management; business owners check reviews daily | Low | v4 Reviews | OAuth2 flow, account/location resolution |
| **Reply to review** | #1 use case for GBP tools; timely responses improve SEO and trust | Low | v4 Reviews | OAuth2, review listing |
| **Read business info** | Must see current state before editing | Low | Business Information v1 | OAuth2, location resolution |
| **Update business hours** | Seasonal changes, holiday hours are frequent ops tasks | Medium | Business Information v1 | OAuth2, location resolution, field mask handling |
| **Update business description** | Profile optimization is table stakes | Low | Business Information v1 | OAuth2, location resolution |
| **Create standard post (What's New)** | Content publishing is core platform promise | Medium | v4 LocalPosts | OAuth2, location resolution, optional media |
| **List posts** | Need to see existing content to avoid duplicates | Low | v4 LocalPosts | OAuth2, location resolution |
| **Delete review reply** | Correct mistakes in automated/manual replies | Low | v4 Reviews | OAuth2, review resolution |
| **OAuth2 connection flow** | User must authorize Google account access | High | Google OAuth2 | Frontend integration page, token storage, refresh handling |

### Notes on Table Stakes

- **Review listing + reply** is the single most valuable feature for business owners. Every GBP management tool starts here.
- **OAuth2 flow** is high complexity because Google requires the `business.manage` scope which is a restricted scope -- the application must pass Google's verification process. For a diploma project, development/testing can use an unverified app with test users (up to 100).
- **Business info reads** validate the connection is working and give the LLM context about what the business currently looks like.

## Differentiators

Features that add value beyond basic management. Not expected, but appreciated.

| Feature | Value Proposition | Complexity | API | Dependencies |
|---------|-------------------|------------|-----|--------------|
| **Create offer post** | Promotions with coupon codes, redemption links, T&C | Medium | v4 LocalPosts | Standard post support, offer-specific fields |
| **Create event post** | Announce events with dates/times, CTA buttons | Medium | v4 LocalPosts | Standard post support, event-specific fields |
| **Update special hours** | Holiday/exception hours separate from regular hours | Medium | Business Information v1 | Business hours update support |
| **Upload business photos** | Visual content management through chat | High | v4 Media | Photo upload flow (startUpload + upload + create), category enum |
| **Get performance insights** | View impressions, calls, website clicks, direction requests | Medium | Performance v1 | Go client exists, date range handling |
| **Search keyword impressions** | What search terms drive customers to the listing | Medium | Performance v1 | Performance API access |
| **Update categories** | Change primary/additional business categories | Low | Business Information v1 | Category ID lookup (categories.list/batchGet) |
| **Update attributes** | Manage business attributes (wheelchair accessible, Wi-Fi, etc.) | Medium | Business Information v1 | Attribute metadata lookup |
| **Delete post** | Clean up outdated or incorrect posts | Low | v4 LocalPosts | Post listing support |
| **Batch review retrieval** | Get reviews across multiple locations at once | Medium | v4 Reviews | Multi-location support |

### Notes on Differentiators

- **Offer and event posts** are natural extensions of standard posts. The LLM orchestrator can decide which post type to use based on user intent ("announce a sale" -> offer post, "we have a meetup" -> event post).
- **Performance insights** provide a read-only analytics view. The Go client library `businessprofileperformance/v1` exists and works, making this relatively straightforward.
- **Photo upload** is high complexity because it requires a multi-step flow: `media.startUpload` to get a data reference, then `media.upload` to send binary data, then optionally associate with a category. This is significantly more complex than a URL-based photo reference in posts.

## Anti-Features

Features to explicitly NOT build.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| **Delete reviews** | Impossible via API. You cannot delete reviews, only reply to them or flag them through the GBP UI. Building this would mislead users. | Clearly document in tool descriptions that review deletion is not possible. Offer "reply to review" and suggest flagging via the GBP dashboard for policy violations. |
| **Q&A management** | API was **discontinued** on November 3, 2025. Building Q&A features would fail at runtime. | Do not register any Q&A tools. If users ask, explain that Google removed this API capability. |
| **Product posts** | Google explicitly states "Product Posts cannot be created via the Google My Business API at this time." | Use standard posts with product information in the text/media instead. |
| **Video upload in posts** | Videos are not supported for posts via the API. | Support photo media only in posts. Clearly state text+photo in tool descriptions. |
| **Location creation/verification** | Extremely complex flow (verification codes, phone calls, postcard). Not a chat-appropriate operation. Requires Google's full verification process. | Assume locations are already verified. The agent operates on existing verified locations only. |
| **Multi-account management** | OneVoice is single-owner deployment. Building multi-account GBP would add authentication complexity with no immediate value. | Support one Google account per business. The OAuth token maps to one account. |
| **FoodMenu management** | Niche feature (restaurants only), adds complexity with limited audience. | Defer unless explicitly requested. Not part of general business presence management. |
| **Lodging-specific features** | Lodging API is for hotels/hospitality only. Niche audience. | Out of scope for general-purpose agent. |

## Feature Dependencies

```
OAuth2 Connection Flow
  |
  +-> Account/Location Resolution (list accounts, list locations)
  |     |
  |     +-> Review Management
  |     |     +-> List Reviews
  |     |     +-> Reply to Review
  |     |     +-> Delete Review Reply
  |     |
  |     +-> Business Info Management
  |     |     +-> Read Business Info
  |     |     +-> Update Description
  |     |     +-> Update Hours (regular)
  |     |     +-> Update Special Hours
  |     |     +-> Update Categories
  |     |     +-> Update Attributes
  |     |
  |     +-> Post Management
  |     |     +-> List Posts
  |     |     +-> Create Standard Post
  |     |     +-> Create Offer Post
  |     |     +-> Create Event Post
  |     |     +-> Delete Post
  |     |
  |     +-> Media Management
  |     |     +-> Upload Photo
  |     |     +-> List Media
  |     |     +-> Delete Media
  |     |
  |     +-> Performance Insights (read-only)
  |           +-> Daily Metrics (impressions, calls, clicks)
  |           +-> Search Keyword Impressions
```

**Root dependency:** Everything requires OAuth2 + account/location resolution first. No GBP operation works without knowing `accounts/{accountId}/locations/{locationId}`.

## API-Specific Constraints

### Rate Limits
| Limit | Value | Notes |
|-------|-------|-------|
| Default QPM (all APIs) | 300 requests/min | Per Google Cloud project |
| Edit limit per profile | 10 edits/min | Non-negotiable, cannot be increased |
| User rate limit | 2,400 queries/min/user/project | Additional constraint |

### OAuth2 Scope
- **Required scope:** `https://www.googleapis.com/auth/business.manage`
- This is a **restricted scope** requiring Google's OAuth app verification
- For development/testing: unverified app allows up to 100 test users
- For production: requires brand verification (2-3 days) + security assessment (weeks)

### API Access Prerequisites
1. Google Account with verified GBP active for 60+ days
2. Submit GBP API contact form ("Application for Basic API Access")
3. Wait for approval (up to 14 days)
4. Receive 300 QPM quota (0 QPM = not approved)
5. Enable all required APIs in Google Cloud Console (8 APIs)

### v4 API Considerations
- Reviews, LocalPosts, Media remain on v4 with no Go client library
- Must implement direct HTTP REST client with OAuth2 bearer tokens
- Base URL: `https://mybusiness.googleapis.com/v4/`
- Same OAuth scope covers all APIs

## MVP Recommendation

### Phase 1: Foundation (must-have)

Build the OAuth2 flow and core agent scaffold:

1. **OAuth2 connection flow** -- Google OAuth2 with `business.manage` scope, token encryption, refresh handling. Reuse existing OAuth patterns from VK agent integration page.
2. **Account/location resolution** -- On connect, discover the user's accounts and locations. Cache the `accounts/{id}/locations/{id}` path needed for all subsequent calls.
3. **List reviews** -- First functional tool. Proves the integration works end-to-end.
4. **Reply to review** -- Highest-value write operation. Business owners care about this most.

### Phase 2: Business Presence (high-value)

5. **Read business info** -- Gives LLM context about the business.
6. **Update description** -- Simple write, validates update flow works.
7. **Update business hours** -- Frequent operation, validates field mask handling.
8. **Create standard post** -- Content publishing, core platform promise.

### Phase 3: Extended (nice-to-have for demo)

9. **List posts** -- Context for post management.
10. **Delete post** -- Cleanup capability.
11. **Create offer/event posts** -- Richer content types.
12. **Performance insights** -- Analytics read, uses Go client library (easier).

### Defer

- **Photo upload**: Multi-step binary upload flow, disproportionate complexity for demo value.
- **Attribute/category updates**: Low-frequency operations, add after core works.
- **Special hours**: Extension of hours management, not critical path.
- **Batch review retrieval**: Multi-location feature, single-owner deployment does not need it.

## Tool Naming Convention

Following existing OneVoice convention (`{platform}__{action}`):

| Tool Name | API | Operation |
|-----------|-----|-----------|
| `google_business__list_reviews` | v4 Reviews | List reviews for location |
| `google_business__reply_to_review` | v4 Reviews | Reply to a specific review |
| `google_business__delete_review_reply` | v4 Reviews | Delete own reply to a review |
| `google_business__get_business_info` | Business Info v1 | Read location details |
| `google_business__update_description` | Business Info v1 | Update business description |
| `google_business__update_hours` | Business Info v1 | Update regular business hours |
| `google_business__create_post` | v4 LocalPosts | Create standard/offer/event post |
| `google_business__list_posts` | v4 LocalPosts | List existing posts |
| `google_business__delete_post` | v4 LocalPosts | Delete a post |
| `google_business__get_insights` | Performance v1 | Fetch daily metrics |

**Platform ID:** `google_business` (maps to NATS subject `tasks.google_business`)

## Competitive Landscape

Tools like Birdeye, EmbedSocial, Ayrshare, and SocialPilot offer GBP management. Their table stakes are:
- Review monitoring and reply
- Post scheduling and creation
- Business info updates
- Multi-location analytics

OneVoice differentiator: conversational interface ("reply to that negative review professionally" vs clicking through a dashboard). The LLM can draft review replies, generate post content, and execute updates -- all in one chat thread alongside Telegram and VK management.

## Available Daily Metrics (Performance API)

For reference when building insights tools:

| Metric Enum | Description |
|-------------|-------------|
| BUSINESS_IMPRESSIONS_DESKTOP_MAPS | Views on Google Maps (desktop) |
| BUSINESS_IMPRESSIONS_DESKTOP_SEARCH | Views on Google Search (desktop) |
| BUSINESS_IMPRESSIONS_MOBILE_MAPS | Views on Google Maps (mobile) |
| BUSINESS_IMPRESSIONS_MOBILE_SEARCH | Views on Google Search (mobile) |
| BUSINESS_CONVERSATIONS | Message conversations received |
| BUSINESS_DIRECTION_REQUESTS | Direction requests to location |
| CALL_CLICKS | Call button clicks |
| WEBSITE_CLICKS | Website link clicks |
| BUSINESS_BOOKINGS | Reserve with Google bookings |
| BUSINESS_FOOD_ORDERS | Food orders received |
| BUSINESS_FOOD_MENU_CLICKS | Menu interaction clicks |

## Post Types Reference

For the LLM tool dispatch and tool descriptions:

| Topic Type | Required Fields | Optional Fields | CTA Options |
|------------|----------------|-----------------|-------------|
| STANDARD | summary | media[], languageCode | Book, Order, Shop, Learn More, Sign Up, Call |
| EVENT | summary, event.title, event.schedule | media[], callToAction | Same as STANDARD |
| OFFER | summary, event (schedule) | offer.couponCode, offer.redeemOnlineUrl, offer.termsConditions, media[] | Same as STANDARD |
| ALERT | summary, alertType | media[] | N/A (system-generated) |

## Review Data Fields

Fields available when listing reviews:

| Field | Type | Notes |
|-------|------|-------|
| reviewId | string | Unique identifier |
| reviewer.displayName | string | Reviewer's display name |
| reviewer.profilePhotoUrl | string | Reviewer's photo URL |
| starRating | enum | ONE through FIVE |
| comment | string | Review text (may be empty for rating-only reviews) |
| createTime | timestamp | When the review was posted |
| updateTime | timestamp | When the review was last updated |
| reviewReply.comment | string | Business owner's reply text |
| reviewReply.updateTime | timestamp | When the reply was posted/updated |

## Location Schema (Key Updatable Fields)

Fields the agent can read/write through Business Information API v1:

| Field | Readable | Writable | Notes |
|-------|----------|----------|-------|
| title (business name) | Yes | Yes | Must match real-world name |
| phone_numbers | Yes | Yes | Primary + up to 2 additional |
| website_uri | Yes | Yes | Business website URL |
| categories (primary + additional) | Yes | Yes | Uses category IDs from categories.list |
| profile.description | Yes | Yes | Business description text |
| storefront_address | Yes | Yes | Physical address |
| regular_hours | Yes | Yes | Standard operating hours |
| special_hours | Yes | Yes | Holiday/exception overrides |
| more_hours | Yes | Yes | Department-specific hours |
| open_info | Yes | Yes | Open, temporarily closed, permanently closed |
| service_area | Yes | Yes | For service-area businesses |
| service_items | Yes | Yes | Services offered with pricing |
| labels | Yes | Yes | Internal tags |
| latlng | Yes | Yes (immutable after create) | Coordinates |
| metadata (place_id, maps_uri) | Yes | No | Read-only Google-generated |
| store_code | Yes | Yes | External reference ID |

## Sources

- [Google Business Profile APIs Overview](https://developers.google.com/my-business) -- HIGH confidence
- [API Reference (REST)](https://developers.google.com/my-business/reference/rest) -- HIGH confidence
- [Review Data Guide](https://developers.google.com/my-business/content/review-data) -- HIGH confidence
- [Posts Data Guide](https://developers.google.com/my-business/content/posts-data) -- HIGH confidence
- [Business Information API v1 RPC Reference](https://developers.google.com/my-business/reference/businessinformation/rpc/google.mybusiness.businessinformation.v1) -- HIGH confidence
- [Performance API Reference](https://developers.google.com/my-business/reference/performance/rest) -- HIGH confidence
- [DailyMetric Enum](https://developers.google.com/my-business/reference/performance/rest/v1/DailyMetric) -- HIGH confidence
- [Media API (v4)](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.media) -- HIGH confidence
- [LocalPosts API (v4)](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.localPosts) -- HIGH confidence
- [Reviews API (v4)](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.reviews) -- HIGH confidence
- [Deprecation Schedule](https://developers.google.com/my-business/content/sunset-dates) -- HIGH confidence
- [Rate Limits](https://developers.google.com/my-business/content/limits) -- HIGH confidence
- [Prerequisites](https://developers.google.com/my-business/content/prereqs) -- HIGH confidence
- [Best Practices](https://developers.google.com/my-business/content/best-practices) -- HIGH confidence
- [FAQ](https://developers.google.com/my-business/content/faq) -- HIGH confidence
- [Go Client: mybusinessbusinessinformation/v1](https://pkg.go.dev/google.golang.org/api/mybusinessbusinessinformation/v1) -- HIGH confidence
- [Go Client: mybusinessaccountmanagement/v1](https://pkg.go.dev/google.golang.org/api/mybusinessaccountmanagement/v1) -- HIGH confidence
- [Go Client: businessprofileperformance/v1](https://pkg.go.dev/google.golang.org/api/businessprofileperformance/v1) -- HIGH confidence
- [deleteReply Method](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.reviews/deleteReply) -- HIGH confidence

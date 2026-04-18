# Roadmap: OneVoice

## Milestones

- ✅ **v1.0 Hardening** — Phases 1-6 (shipped 2026-03-20) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Observability & Debugging** — Phases 7-9 (shipped 2026-03-22) — [archive](milestones/v1.1-ROADMAP.md)
- 🚧 **v1.2 Google Business Profile** — Phases 10-14 (in progress)

## Phases

<details>
<summary>✅ v1.0 Hardening (Phases 1-6) — SHIPPED 2026-03-20</summary>

- [x] Phase 1: Security Foundation (4/4 plans) — completed 2026-03-15
- [x] Phase 2: Reliability Foundation (4/4 plans) — completed 2026-03-16
- [x] Phase 3: VK Agent Completion (5/5 plans) — completed 2026-03-19
- [x] Phase 4: Yandex.Business Agent (5/5 plans) — completed 2026-03-19
- [x] Phase 5: Observability (4/4 plans) — completed 2026-03-20
- [x] Phase 6: Testing Completion (2/2 plans) — completed 2026-03-20

</details>

<details>
<summary>✅ v1.1 Observability & Debugging (Phases 7-9) — SHIPPED 2026-03-22</summary>

- [x] Phase 7: Backend Logging Gaps (2/2 plans) — completed 2026-03-22
- [x] Phase 8: Grafana + Loki Stack (2/2 plans) — completed 2026-03-22
- [x] Phase 9: Frontend Telemetry (2/2 plans) — completed 2026-03-22

</details>

### 🚧 v1.2 Google Business Profile (In Progress)

**Milestone Goal:** Add Google Business Profile as a fully integrated platform agent — users connect their Google account and manage reviews, business info, posts, photos, and performance through the conversational chat interface.

- [x] **Phase 10: OAuth + Token Infrastructure + Agent Scaffold** - Google OAuth2 flow, automatic token refresh, agent service skeleton with NATS dispatch (completed 2026-04-08)
- [ ] **Phase 11: Review Management + End-to-End Wiring** - First working tools (list/reply/delete reviews), orchestrator registration, frontend integration page
- [ ] **Phase 12: Business Information Management** - Read and update business description, hours via v1 API
- [ ] **Phase 13: Post Management** - Create, list, delete posts (standard, offer, event types) via v4 API
- [ ] **Phase 14: Media Upload + Performance Insights** - Photo upload (3-step flow), read-only performance metrics

## Phase Details

### Phase 10: OAuth + Token Infrastructure + Agent Scaffold
**Goal**: Users can connect their Google Business Profile account and the system maintains valid API access indefinitely
**Depends on**: Nothing (first phase of v1.2; builds on existing OAuth patterns from VK)
**Requirements**: INFRA-01, INFRA-02, INFRA-03, INTEG-01
**Success Criteria** (what must be TRUE):
  1. User can initiate Google OAuth2 from the API and receive an access token + refresh token stored encrypted in the database
  2. After OAuth completes, the system automatically discovers and stores the user's business account and location IDs
  3. When a Google access token expires (1hr), the next API request transparently refreshes it without user action
  4. The agent-google-business service starts, connects to NATS, and responds to `tasks.google_business` subjects
**Plans**: 2/3 complete
**UI hint**: yes

### Phase 11: Review Management + End-to-End Wiring
**Goal**: Users can manage Google reviews through the chat interface with full end-to-end integration from frontend to agent
**Depends on**: Phase 10
**Requirements**: REV-01, REV-02, REV-03, INTEG-02, INTEG-03
**Success Criteria** (what must be TRUE):
  1. User can ask the LLM to list recent reviews and see review text, rating, and author in the chat response
  2. User can ask the LLM to reply to a specific review and the reply appears on Google
  3. User can ask the LLM to delete a review reply and it is removed from Google
  4. Google Business tools (`google_business__*`) appear in the orchestrator's available tools when the user has a Google integration
  5. User can connect and disconnect their Google Business account from the frontend integrations page
**Plans**: TBD
**UI hint**: yes

### Phase 12: Business Information Management
**Goal**: Users can view and update their Google business details through the chat interface
**Depends on**: Phase 11
**Requirements**: BINFO-01, BINFO-02, BINFO-03
**Success Criteria** (what must be TRUE):
  1. User can ask the LLM for current business info and see description, hours, phone, and website in the response
  2. User can ask the LLM to update the business description and the change is reflected on Google
  3. User can ask the LLM to update business hours and the change is reflected on Google
**Plans**: TBD

### Phase 13: Post Management
**Goal**: Users can create, browse, and remove Google Business posts of all types through the chat interface
**Depends on**: Phase 11
**Requirements**: POST-01, POST-02, POST-03, POST-04, POST-05
**Success Criteria** (what must be TRUE):
  1. User can ask the LLM to create a "What's New" post and it appears on Google
  2. User can ask the LLM to list existing posts and see their content and type
  3. User can ask the LLM to delete a post and it is removed from Google
  4. User can ask the LLM to create an offer post with coupon code and terms
  5. User can ask the LLM to create an event post with title, date, and time
**Plans**: TBD

### Phase 14: Media Upload + Performance Insights
**Goal**: Users can upload business photos and view performance metrics through the chat interface
**Depends on**: Phase 11
**Requirements**: MEDIA-01, PERF-01
**Success Criteria** (what must be TRUE):
  1. User can ask the LLM to upload a photo and it appears in the business's Google photo gallery
  2. User can ask the LLM for performance metrics and see impressions, clicks, and calls data in the response
**Plans**: TBD

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|---------------|--------|-----------|
| 1. Security Foundation | v1.0 | 4/4 | Complete | 2026-03-15 |
| 2. Reliability Foundation | v1.0 | 4/4 | Complete | 2026-03-16 |
| 3. VK Agent Completion | v1.0 | 5/5 | Complete | 2026-03-19 |
| 4. Yandex.Business Agent | v1.0 | 5/5 | Complete | 2026-03-19 |
| 5. Observability | v1.0 | 4/4 | Complete | 2026-03-20 |
| 6. Testing Completion | v1.0 | 2/2 | Complete | 2026-03-20 |
| 7. Backend Logging Gaps | v1.1 | 2/2 | Complete | 2026-03-22 |
| 8. Grafana + Loki Stack | v1.1 | 2/2 | Complete | 2026-03-22 |
| 9. Frontend Telemetry | v1.1 | 2/2 | Complete | 2026-03-22 |
| 10. OAuth + Token Infrastructure + Agent Scaffold | v1.2 | 3/3 | Complete   | 2026-04-08 |
| 11. Review Management + End-to-End Wiring | v1.2 | 0/0 | Not started | - |
| 12. Business Information Management | v1.2 | 0/0 | Not started | - |
| 13. Post Management | v1.2 | 0/0 | Not started | - |
| 14. Media Upload + Performance Insights | v1.2 | 0/0 | Not started | - |

---
*Last updated: 2026-04-08*

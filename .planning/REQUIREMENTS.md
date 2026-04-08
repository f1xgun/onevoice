# Requirements: OneVoice

**Defined:** 2026-04-08
**Core Value:** Business owners can manage digital presence across platforms through a single conversational interface

## v1.2 Requirements

Requirements for Google Business Profile integration. Each maps to roadmap phases.

### Infrastructure

- [x] **INFRA-01**: User can connect Google account via OAuth2 on integrations page
- [x] **INFRA-02**: System auto-discovers user's business locations after OAuth connection
- [x] **INFRA-03**: System auto-refreshes expired Google tokens (1hr expiry) without user intervention

### Review Management

- [ ] **REV-01**: User can list recent reviews for their business via chat
- [ ] **REV-02**: User can reply to a specific review via chat
- [ ] **REV-03**: User can delete their reply to a review via chat

### Business Information

- [ ] **BINFO-01**: User can view current business info (description, hours, phone, website) via chat
- [ ] **BINFO-02**: User can update business description via chat
- [ ] **BINFO-03**: User can update business hours via chat

### Post Management

- [ ] **POST-01**: User can create a standard "What's New" post via chat
- [ ] **POST-02**: User can list existing posts via chat
- [ ] **POST-03**: User can delete a post via chat
- [ ] **POST-04**: User can create offer post with coupon/terms via chat
- [ ] **POST-05**: User can create event post with date/time via chat

### Media

- [ ] **MEDIA-01**: User can upload business photos via chat

### Performance

- [ ] **PERF-01**: User can view business performance metrics (impressions, clicks, calls) via chat

### System Integration

- [ ] **INTEG-01**: Google Business agent runs as standalone Go microservice with NATS dispatch
- [ ] **INTEG-02**: Orchestrator registers `google_business__*` tools for LLM dispatch
- [ ] **INTEG-03**: Frontend shows Google Business on integrations page with connect/disconnect

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Extended Business Info

- **BINFO-04**: User can update business categories via chat
- **BINFO-05**: User can update business attributes (Wi-Fi, accessibility, etc.) via chat
- **BINFO-06**: User can manage special/holiday hours via chat

### Extended Posts

- **POST-06**: User can create alert posts via chat

### Multi-location

- **MLOC-01**: User can manage multiple business locations from one account
- **MLOC-02**: User can batch-retrieve reviews across locations

## Out of Scope

| Feature | Reason |
|---------|--------|
| Review deletion | Impossible via API — only reply management |
| Q&A management | API discontinued Nov 3, 2025 |
| Video in posts | Not supported via Google API |
| Location creation/verification | Too complex for chat, assume pre-verified locations |
| Google Maps embed in frontend | Not needed, only API management |
| Product posts | Google explicitly blocks product posts via API |
| FoodMenu management | Niche (restaurants only), limited audience |
| Lodging features | Niche (hotels only), out of scope |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| INFRA-01 | Phase 10 | Complete |
| INFRA-02 | Phase 10 | Complete |
| INFRA-03 | Phase 10 | Complete |
| REV-01 | Phase 11 | Pending |
| REV-02 | Phase 11 | Pending |
| REV-03 | Phase 11 | Pending |
| BINFO-01 | Phase 12 | Pending |
| BINFO-02 | Phase 12 | Pending |
| BINFO-03 | Phase 12 | Pending |
| POST-01 | Phase 13 | Pending |
| POST-02 | Phase 13 | Pending |
| POST-03 | Phase 13 | Pending |
| POST-04 | Phase 13 | Pending |
| POST-05 | Phase 13 | Pending |
| MEDIA-01 | Phase 14 | Pending |
| PERF-01 | Phase 14 | Pending |
| INTEG-01 | Phase 10 | Pending |
| INTEG-02 | Phase 11 | Pending |
| INTEG-03 | Phase 11 | Pending |

**Coverage:**
- v1.2 requirements: 19 total
- Mapped to phases: 19
- Unmapped: 0

---
*Requirements defined: 2026-04-08*
*Last updated: 2026-04-08 after roadmap creation*

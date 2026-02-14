# CLAUDE.md

**⚠️ CRITICAL: This worktree follows mandatory development workflow defined in `WORKFLOW.md`**
**Always check @WORKFLOW.md before starting any task!**

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**OneVoice** — platform-agnostic multi-agent system with hybrid integration model (API + RPA) for automating digital presence management for SMBs (малый и средний бизнес). Currently in the **thesis documentation phase** (ВКР — выпускная квалификационная работа). No implementation code exists yet; the repository contains product specification, thesis roadmap, and a multi-agent workspace for writing the thesis document. The Russian market serves as the primary case study (VK, Telegram, Yandex.Business), but the architecture is universal.

All documentation is in **Russian**. Academic/scientific writing style is required for thesis output.

## Repository Structure

- `prd/PRD.md` — Product Requirements Document (v2.1, ~95KB). Full system specification including architecture, API design, integrations, security, monetization.
- `docs/VKR_ROADMAP.md` — Thesis roadmap with chapter-by-chapter tasks, rules for LLM agents, GOST formatting requirements, minimum deliverable thresholds.
- `vkr/` — Multi-agent workspace for thesis document preparation:
  - `draft/` — Working thesis draft (`draft.md`) and table of contents (`toc.md`)
  - `tasks/<task_id>/` — Individual section "tickets" with `brief.md`, `evidence.md`, `factcheck.md`, `output.md`, `status.md`
  - `sources/sources.md` — Global bibliography registry (GOST-formatted, `[n]` citation numbering)
  - `sources/claims.md` — Claims/assertions log
  - `decisions/` — `decisions.md` (approved), `open_questions.md` (blocking), `assumptions.md` (working)
  - `prompts/` — Agent role definitions: `RESEARCH_AGENT.md`, `FACTCHECK_AGENT.md`, `MAIN_EDITOR.md`
  - `orchestration/WORKFLOW.md` — Status workflow and orchestration rules

## Multi-Agent Thesis Workflow

Three agent roles collaborate on each thesis section:

1. **ResearchAgent** → fills `tasks/<id>/evidence.md` with sourced, atomic claims (min 2 independent sources per fact)
2. **FactcheckAgent** → verifies claims in `tasks/<id>/factcheck.md`, marks each as `ok` / `needs_fix` / `reject`
3. **MainEditor** → writes `tasks/<id>/output.md` using only verified claims, manages `sources/sources.md`, assembles `draft/draft.md`

### Status Transitions

`pending` → `researching` → `research_done` → `factchecking` → `factcheck_ok` → `writing` → `ready`

Alternate paths: `factcheck_needs_more` loops back to `researching`; `blocked_user` requires user decision via `decisions/open_questions.md`.

### Parallelism Rule (5+5+1)

- Up to 5 tasks in `researching` simultaneously
- Up to 5 tasks in `factchecking` simultaneously
- Only 1 task in `writing` at a time (to maintain consistent style and `[n]` numbering)

### Citation Policy

- `[n]` references are globally numbered by order of first appearance in `draft/draft.md`
- All sources registered in `vkr/sources/sources.md`
- Reuse existing `[n]` if source already exists; append new sources at end
- No local source lists in `output.md` — global registry only

## Writing Rules

- Scientific style, impersonal constructions (безличные конструкции)
- Expand abbreviations on first use
- Avoid anglicisms when Russian equivalents exist
- All facts/figures/comparisons must have `[n]` citations
- Source recency: technology/protocols ≤ 3 years; statistics/market ≤ 5 years
- Only `ok` or corrected `needs_fix` claims may appear in `output.md`

## Planned Tech Stack (for implementation phase)

- **Backend:** Go 1.22+, NATS JetStream, MCP/A2A agent protocol
- **Frontend:** Next.js 14 + React 18 + Tailwind CSS + shadcn/ui
- **Database:** PostgreSQL 16, Redis 7, MinIO (S3)
- **Infrastructure:** Docker Compose (dev), Kubernetes (prod), GitHub Actions CI/CD
- **LLM:** OpenAI GPT-4 / Claude 3 for orchestration
- **Integrations (API-based):** VK, Telegram, Google Business Profile (international)
- **Integrations (RPA-based):** Яндекс.Бизнес (Playwright), 2ГИС (Playwright)
- **RPA Engine:** Playwright for browser automation of platforms without public APIs

## Key Decisions (Approved)

1. Domain example: coffee shop (кофейня)
2. Tech stack: Go + Next.js/React + PostgreSQL
3. Competitors for analysis: Bitrix24, SendPulse, Kommo, Hootsuite, Buffer, SMMplanner, YouScan
4. Hybrid integration model: API-based agents (VK, Telegram) + RPA-based agents (Yandex.Business via Playwright) in MVP
5. Instagram removed from project scope (Meta recognized as extremist org in Russia, legally blocked)
6. Google Business Profile deprioritized to Phase 3 (only 3% usage in Russia)

## Thesis Requirements

- Minimum 80 pages (excluding appendices)
- Minimum 50 sources (≤50% electronic, ≥5% foreign-language)
- Minimum deliverables: 10 DB tables, 500 records, 20 UI forms, 3 user roles, 5 algorithms, 30+ functions
- Formatting per GOST standards (see `docs/` for methodological guidelines)

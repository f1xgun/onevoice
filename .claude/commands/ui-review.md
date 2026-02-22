---
description: Run UI/UX review on changed frontend pages using Playwright MCP
allowed-tools: Bash, Read, Grep, Glob, mcp__playwright__browser_navigate, mcp__playwright__browser_snapshot, mcp__playwright__browser_take_screenshot, mcp__playwright__browser_resize, mcp__playwright__browser_click, mcp__playwright__browser_console_messages, mcp__playwright__browser_evaluate, mcp__playwright__browser_wait_for, mcp__playwright__browser_close
---

# UI/UX Review

You are now acting as the **UI Reviewer** for OneVoice frontend.

Read the full agent definition and rules from `.claude/agents/ui-reviewer.md`, then execute the review process described there.

## Quick start

1. **Check dev server** — verify `http://localhost:3000` is reachable. If not, tell the user to start it.
2. **Determine scope** — look at recent `git diff` for `services/frontend/` to find which pages changed. If the user provided specific pages as arguments, review those instead.
3. **Run the review** — follow the 3-viewport review process (desktop 1440x900, tablet 768x1024, mobile 375x812) for each affected page.
4. **Report findings** — output the structured report as defined in the agent rules.

Arguments (optional): page paths to review, e.g. `/ui-review /integrations /business`

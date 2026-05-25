# Runbook — Email DNS setup (Phase 21a)

**Audience:** Operator deploying OneVoice to production.
**Blocking:** Phase 21b (password reset) and 21c (email verification) integration tests rely on real email delivery to `@yandex.ru` and `@mail.ru` accounts. RU mailbox providers silently spam-folder any mail lacking SPF/DKIM/DMARC. **Do not start 21b/21c testing until every check in §6 passes.**
**From-address:** `noreply@onevoice.app` (locked in CONTEXT D-04).

## 1. Prerequisites

- Domain `onevoice.app` already registered and managed in your DNS provider (Cloudflare, route53, or any registrar dashboard).
- Unisender Go account created at https://go.unisender.ru/. Free tier ~₽250/10k emails covers v1.4 beta.
- Access to the DNS zone for `onevoice.app` (TXT record write permission).

## 2. Add the sending domain in Unisender Go

1. Sign in to https://go.unisender.ru/.
2. **Settings → Domains → Add domain.**
3. Enter `onevoice.app`. Unisender will generate three DNS records:
   - SPF (TXT at apex)
   - DKIM (TXT at a selector subdomain, e.g. `mail._domainkey.onevoice.app`)
   - DMARC (TXT at `_dmarc.onevoice.app`)
4. Copy the EXACT values shown — they are tied to your Unisender account.

## 3. Add the DNS records

Paste the records into your DNS provider. Example shape (the `v=DKIM1` selector and `p=` public key are account-specific):

| Host | Type | Value (example shape) | TTL |
|------|------|-----------------------|-----|
| `onevoice.app` | TXT | `v=spf1 include:_spf.unisender.ru ~all` | 3600 |
| `mail._domainkey.onevoice.app` | TXT | `v=DKIM1; k=rsa; p=MIGfMA0GCSq...` (from Unisender) | 3600 |
| `_dmarc.onevoice.app` | TXT | `v=DMARC1; p=quarantine; rua=mailto:dmarc@onevoice.app; pct=100` | 3600 |

**Notes:**

- SPF: use `~all` (softfail) initially; tighten to `-all` (hardfail) only after one full month of clean delivery.
- DMARC: start with `p=quarantine`. Switch to `p=reject` only after you have a stable DMARC aggregate-report inbox AND ≥30 days of green RUA reports.
- One DKIM key per domain is enough for Phase 21. Phase 22 will reuse the same key for the `pdn@onevoice.app` mailbox (D-04).

## 4. Add the from-mailbox

1. Provision the mailbox `noreply@onevoice.app` on your mail provider (Yandex 360, Google Workspace, or any SMTP relay you trust). It does NOT need to receive replies — Unisender uses the from-address only as a header.
2. In Unisender Go: **Settings → Senders → Add sender.** Enter `noreply@onevoice.app` + display name `OneVoice`.
3. Unisender sends a confirmation email to that mailbox. Click the confirm link. Until you click, ALL sends from that address return 403 `unverified_sender`.

## 5. Wait for propagation

DNS TTL is usually 300-3600 seconds. Cloud DNS providers (Cloudflare, route53) are near-instant; some registrars take up to 24h. Continue to §6 once `dig` returns the records.

## 6. Verification checklist

Run each command from any machine with `dig` installed. ALL must return the expected record before unblocking Phase 21b/21c integration tests.

```bash
# SPF — should show the Unisender include directive.
dig +short TXT onevoice.app | grep -E 'v=spf1.*_spf\.unisender\.ru'

# DKIM — should show a v=DKIM1 record with a non-empty p= public key.
dig +short TXT mail._domainkey.onevoice.app | grep -E 'v=DKIM1.*p='

# DMARC — should show v=DMARC1 with a policy and rua mailbox.
dig +short TXT _dmarc.onevoice.app | grep -E 'v=DMARC1.*p=(quarantine|reject)'
```

Then end-to-end test:

```bash
# In the Unisender Go dashboard: Domains → onevoice.app → "Validate".
# Expected: SPF green, DKIM green, DMARC green.

# Test send from the Unisender console to a real @yandex.ru and a real
# @mail.ru account you control. Check the inbox (NOT spam):
#   - Yandex.Mail: open the message → ⋯ → "Properties of message" →
#     "DKIM signature" should read "Verified".
#   - Mail.ru:    open the message → menu → "View original" → headers
#     should show "Authentication-Results: ... spf=pass dkim=pass dmarc=pass".

# If either provider shows dkim=fail or routes to spam:
#   1. Re-check that the DNS TTL has expired and `dig` returns the
#      record from §6.
#   2. Re-check the DKIM public key in Unisender matches what is in DNS
#      (one stray newline breaks the chain).
#   3. Run https://www.mail-tester.com/ — get a score of 9/10 or
#      higher before proceeding. Anything lower means a misconfig.
```

## 7. After verification passes

1. Set the env vars in `.env.prod` on the VM:
   - `UNISENDER_API_KEY=<key from Unisender Go: Settings → API keys>`
   - `UNISENDER_FROM_EMAIL=noreply@onevoice.app`
   - `UNISENDER_FROM_NAME=OneVoice`
2. Restart the API: `docker compose up -d --force-recreate api`.
3. API startup log should show `email: UnisenderSender constructed from_email=noreply@onevoice.app`. If you instead see `email: UNISENDER_API_KEY not set — using NoopSender`, the env var didn't load.
4. Unblock Phase 21b/21c integration tests.

## 8. Ongoing monitoring

- Add a Grafana alert on the metric `email_outbox_failed_rows_total` (when Prometheus instrumentation lands in v1.4.x) — non-zero failed rows for > 1h means delivery is broken.
- Set up DMARC RUA aggregate-report ingestion (out of scope for v1.4; track in v1.5 backlog).
- Rotate the Unisender API key annually. Old keys do not invalidate sends in-flight.

## 9. Rollback

If deliverability suddenly drops and DKIM/SPF/DMARC are the suspect:

1. Set `UNISENDER_API_KEY=` (empty) in `.env.prod`. API falls back to NoopSender — no production emails sent. Password-reset / verify flows become broken (acceptable during emergency).
2. Re-run §6 checks; whatever is broken in DNS, fix.
3. Set the API key back and restart.

---

*Phase 21a operator handoff. Update this runbook whenever Unisender's DNS record shape changes.*

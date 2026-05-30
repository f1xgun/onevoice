// Package templates owns the Russian-primary email body builders for
// transactional emails. Each template ships both a plain-text
// fallback (REQUIRED by Unisender Go ) and an HTML
// variant. Linen palette is rendered as hex fallbacks because email
// clients don't process OKLCH.
package templates

import (
	"fmt"
	"time"
)

// DeletionConfirmationSubject is the verbatim subject line from
// UI-SPEC Surface 12 /. Exported as a constant so
// the integration tests can assert against the outbox row.
const DeletionConfirmationSubject = "Удаление аккаунта запланировано — OneVoice"

// DeletionT7WarningSubject is the verbatim subject line from
// UI-SPEC Surface 12 /. Exported as a constant so
// the warning sweeper can dedupe via ExistsBySubjectAndRecipient.
const DeletionT7WarningSubject = "Удаление аккаунта — осталось 7 дней"

// DeletionConfirmationText builds the plain-text body sent immediately
// after a successful DELETE /users/me. Verbatim RU copy from CONTEXT
// + UI-SPEC Surface 12.
//
// deletionAt is requestedAt + 30 days — the moment the hard-delete
// sweeper will run.
func DeletionConfirmationText(email string, deletionAt time.Time) string {
	return fmt.Sprintf(`Здравствуйте!

Вы запросили удаление аккаунта в OneVoice. Окончательное удаление произойдёт %s (через 30 дней).

В течение этого срока вы можете отменить удаление, войдя в аккаунт:
https://onevoice.app/settings/account

Через 30 дней данные удалятся безвозвратно.

С уважением,
команда OneVoice
`, deletionAt.Format("02.01.2006"))
}

// DeletionConfirmationHTML mirrors the text version. Inline Linen palette
// hex fallbacks (no class= rules — email clients strip <style>).
func DeletionConfirmationHTML(email string, deletionAt time.Time) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru"><body style="background:#F5E9D9;color:#2C2520;font-family:Arial,sans-serif;padding:32px;">
<h1 style="font-size:24px;font-weight:500;margin:0 0 16px 0;">Удаление аккаунта запланировано</h1>
<p style="font-size:15px;line-height:1.55;margin:0 0 16px 0;">
Здравствуйте! Вы запросили удаление аккаунта в OneVoice. Окончательное удаление произойдёт
<strong>%s</strong> (через 30 дней).
</p>
<p style="font-size:15px;line-height:1.55;margin:0 0 24px 0;">
В течение этого срока вы можете отменить удаление, войдя в аккаунт.
</p>
<a href="https://onevoice.app/settings/account" style="display:inline-block;background:#D89B5A;color:#2C2520;padding:12px 24px;border-radius:10px;text-decoration:none;font-weight:500;">Открыть настройки аккаунта</a>
<p style="font-size:13px;color:#6B5C50;margin:32px 0 0 0;">
Через 30 дней данные удалятся безвозвратно.<br>С уважением, команда OneVoice
</p>
</body></html>
`, deletionAt.Format("02.01.2006"))
}

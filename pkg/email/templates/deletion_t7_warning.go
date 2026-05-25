package templates

import (
	"fmt"
	"time"
)

// DeletionT7WarningText is the T-7 warning email plain-text body. Sent
// 23 days after deletion_requested_at (so the recipient sees it 7 days
// before the hard-delete sweeper runs). Verbatim RU copy from
// UI-SPEC Surface 12 / Phase 21-04 / D-35.
//
// deletionAt is requestedAt + 30 days — the moment the sweeper will run.
func DeletionT7WarningText(email string, deletionAt time.Time) string {
	return fmt.Sprintf(`Здравствуйте!

Через 7 дней (%s) ваш аккаунт OneVoice будет удалён без возможности восстановления.

Если это решение принято в спешке — войдите и нажмите «Отменить удаление»:
https://onevoice.app/settings/account

После %s данные удалятся безвозвратно.

С уважением,
команда OneVoice
`, deletionAt.Format("02.01.2006"), deletionAt.Format("02.01.2006"))
}

// DeletionT7WarningHTML is the HTML variant. Inline Linen palette per
// the email-rendering convention.
func DeletionT7WarningHTML(email string, deletionAt time.Time) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru"><body style="background:#F5E9D9;color:#2C2520;font-family:Arial,sans-serif;padding:32px;">
<h1 style="font-size:24px;font-weight:500;margin:0 0 16px 0;color:#B0432F;">Удаление аккаунта — осталось 7 дней</h1>
<p style="font-size:15px;line-height:1.55;margin:0 0 16px 0;">
Здравствуйте! Через 7 дней (<strong>%s</strong>) ваш аккаунт OneVoice будет удалён без возможности восстановления.
</p>
<p style="font-size:15px;line-height:1.55;margin:0 0 24px 0;">
Если это решение принято в спешке — войдите и отмените удаление.
</p>
<a href="https://onevoice.app/settings/account" style="display:inline-block;background:#D89B5A;color:#2C2520;padding:12px 24px;border-radius:10px;text-decoration:none;font-weight:500;">Войти и отменить</a>
<p style="font-size:13px;color:#6B5C50;margin:32px 0 0 0;">
После %s данные удалятся безвозвратно.<br>С уважением, команда OneVoice
</p>
</body></html>
`, deletionAt.Format("02.01.2006"), deletionAt.Format("02.01.2006"))
}

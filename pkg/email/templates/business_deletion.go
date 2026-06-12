package templates

import (
	"fmt"
	"time"
)

// BusinessDeletionConfirmationSubject is the subject line for the
// organization-deletion confirmation email. Exported so integration tests can
// assert against the outbox row.
const BusinessDeletionConfirmationSubject = "Удаление организации запланировано — OneVoice"

// BusinessDeletionT7WarningSubject is the subject line for the organization
// T-7 warning email. Exported so the sweeper/dedupe logic can reference it.
const BusinessDeletionT7WarningSubject = "Удаление организации — осталось 7 дней"

// BusinessDeletionConfirmationText builds the plain-text body sent immediately
// after a successful DELETE /businesses/{id}. name is the organization name;
// deletionAt is requestedAt + 30 days — the moment the hard-delete sweeper runs.
func BusinessDeletionConfirmationText(name string, deletionAt time.Time) string {
	return fmt.Sprintf(`Здравствуйте!

Вы запросили удаление организации «%s» в OneVoice. Окончательное удаление произойдёт %s (через 30 дней).

В течение этого срока вы можете отменить удаление в настройках организации:
https://onevoice.app/business

Через 30 дней все данные организации удалятся безвозвратно.

С уважением,
команда OneVoice
`, name, deletionAt.Format("02.01.2006"))
}

// BusinessDeletionConfirmationHTML mirrors the text version. Inline Linen
// palette hex fallbacks (no class= rules — email clients strip <style>).
func BusinessDeletionConfirmationHTML(name string, deletionAt time.Time) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru"><body style="background:#F5E9D9;color:#2C2520;font-family:Arial,sans-serif;padding:32px;">
<h1 style="font-size:24px;font-weight:500;margin:0 0 16px 0;">Удаление организации запланировано</h1>
<p style="font-size:15px;line-height:1.55;margin:0 0 16px 0;">
Здравствуйте! Вы запросили удаление организации <strong>%s</strong> в OneVoice. Окончательное удаление произойдёт
<strong>%s</strong> (через 30 дней).
</p>
<p style="font-size:15px;line-height:1.55;margin:0 0 24px 0;">
В течение этого срока вы можете отменить удаление в настройках организации.
</p>
<a href="https://onevoice.app/business" style="display:inline-block;background:#D89B5A;color:#2C2520;padding:12px 24px;border-radius:10px;text-decoration:none;font-weight:500;">Открыть настройки организации</a>
<p style="font-size:13px;color:#6B5C50;margin:32px 0 0 0;">
Через 30 дней все данные организации удалятся безвозвратно.<br>С уважением, команда OneVoice
</p>
</body></html>
`, name, deletionAt.Format("02.01.2006"))
}

// BusinessDeletionT7WarningText is the organization T-7 warning email
// plain-text body. Sent 23 days after deletion_requested_at (7 days before the
// hard-delete sweeper runs). deletionAt is requestedAt + 30 days.
func BusinessDeletionT7WarningText(name string, deletionAt time.Time) string {
	return fmt.Sprintf(`Здравствуйте!

Через 7 дней (%s) организация «%s» в OneVoice будет удалена без возможности восстановления.

Если это решение принято в спешке — откройте настройки организации и нажмите «Отменить удаление»:
https://onevoice.app/business

После %s все данные организации удалятся безвозвратно.

С уважением,
команда OneVoice
`, deletionAt.Format("02.01.2006"), name, deletionAt.Format("02.01.2006"))
}

// BusinessDeletionT7WarningHTML is the HTML variant. Inline Linen palette per
// the email-rendering convention.
func BusinessDeletionT7WarningHTML(name string, deletionAt time.Time) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru"><body style="background:#F5E9D9;color:#2C2520;font-family:Arial,sans-serif;padding:32px;">
<h1 style="font-size:24px;font-weight:500;margin:0 0 16px 0;color:#B0432F;">Удаление организации — осталось 7 дней</h1>
<p style="font-size:15px;line-height:1.55;margin:0 0 16px 0;">
Здравствуйте! Через 7 дней (<strong>%s</strong>) организация <strong>%s</strong> в OneVoice будет удалена без возможности восстановления.
</p>
<p style="font-size:15px;line-height:1.55;margin:0 0 24px 0;">
Если это решение принято в спешке — откройте настройки организации и отмените удаление.
</p>
<a href="https://onevoice.app/business" style="display:inline-block;background:#D89B5A;color:#2C2520;padding:12px 24px;border-radius:10px;text-decoration:none;font-weight:500;">Открыть настройки организации</a>
<p style="font-size:13px;color:#6B5C50;margin:32px 0 0 0;">
После %s все данные организации удалятся безвозвратно.<br>С уважением, команда OneVoice
</p>
</body></html>
`, deletionAt.Format("02.01.2006"), name, deletionAt.Format("02.01.2006"))
}

package templates

import (
	"fmt"
	"time"
)

// localeEN reports whether a user PreferredLocale string selects the English
// copy. Anything other than "en" falls back to the Russian primary, matching
// the users.preferred_locale CHECK constraint ('ru' | 'en', default 'ru').
func localeEN(locale string) bool {
	return locale == "en"
}

// fmtDeletionDate renders the scheduled hard-delete date in a locale-appropriate
// format (RU dd.mm.yyyy, EN "Month D, YYYY").
func fmtDeletionDate(locale string, t time.Time) string {
	if localeEN(locale) {
		return t.Format("January 2, 2006")
	}
	return t.Format("02.01.2006")
}

// BusinessDeletionConfirmationSubject is the subject line for the
// organization-deletion confirmation email, in the owner's locale.
func BusinessDeletionConfirmationSubject(locale string) string {
	if localeEN(locale) {
		return "Organization deletion scheduled — OneVoice"
	}
	return "Удаление организации запланировано — OneVoice"
}

// BusinessDeletionT7WarningSubject is the subject line for the organization
// T-7 warning email, in the owner's locale.
func BusinessDeletionT7WarningSubject(locale string) string {
	if localeEN(locale) {
		return "Organization deletion — 7 days left"
	}
	return "Удаление организации — осталось 7 дней"
}

// BusinessDeletionConfirmationText builds the plain-text body sent immediately
// after a successful DELETE /businesses/{id}, in the owner's locale. name is the
// organization name; deletionAt is requestedAt + 30 days — the moment the
// hard-delete sweeper runs.
func BusinessDeletionConfirmationText(locale, name string, deletionAt time.Time) string {
	date := fmtDeletionDate(locale, deletionAt)
	if localeEN(locale) {
		return fmt.Sprintf(`Hello!

You requested deletion of the organization "%s" in OneVoice. Final deletion will happen on %s (in 30 days).

You can cancel the deletion any time before then in your organization settings:
https://onevoice.app/business

After 30 days all organization data is erased permanently.

Best regards,
the OneVoice team
`, name, date)
	}
	return fmt.Sprintf(`Здравствуйте!

Вы запросили удаление организации «%s» в OneVoice. Окончательное удаление произойдёт %s (через 30 дней).

В течение этого срока вы можете отменить удаление в настройках организации:
https://onevoice.app/business

Через 30 дней все данные организации удалятся безвозвратно.

С уважением,
команда OneVoice
`, name, date)
}

// BusinessDeletionConfirmationHTML mirrors the text version, in the owner's
// locale. Inline Linen palette hex fallbacks (no class= rules — email clients
// strip <style>).
func BusinessDeletionConfirmationHTML(locale, name string, deletionAt time.Time) string {
	date := fmtDeletionDate(locale, deletionAt)
	if localeEN(locale) {
		return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><body style="background:#F5E9D9;color:#2C2520;font-family:Arial,sans-serif;padding:32px;">
<h1 style="font-size:24px;font-weight:500;margin:0 0 16px 0;">Organization deletion scheduled</h1>
<p style="font-size:15px;line-height:1.55;margin:0 0 16px 0;">
Hello! You requested deletion of the organization <strong>%s</strong> in OneVoice. Final deletion will happen on
<strong>%s</strong> (in 30 days).
</p>
<p style="font-size:15px;line-height:1.55;margin:0 0 24px 0;">
You can cancel the deletion any time before then in your organization settings.
</p>
<a href="https://onevoice.app/business" style="display:inline-block;background:#D89B5A;color:#2C2520;padding:12px 24px;border-radius:10px;text-decoration:none;font-weight:500;">Open organization settings</a>
<p style="font-size:13px;color:#6B5C50;margin:32px 0 0 0;">
After 30 days all organization data is erased permanently.<br>Best regards, the OneVoice team
</p>
</body></html>
`, name, date)
	}
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
`, name, date)
}

// BusinessDeletionT7WarningText is the organization T-7 warning email plain-text
// body, in the owner's locale. Sent 23 days after deletion_requested_at (7 days
// before the hard-delete sweeper runs). deletionAt is requestedAt + 30 days.
func BusinessDeletionT7WarningText(locale, name string, deletionAt time.Time) string {
	date := fmtDeletionDate(locale, deletionAt)
	if localeEN(locale) {
		return fmt.Sprintf(`Hello!

In 7 days (%s) the organization "%s" in OneVoice will be deleted with no way to restore it.

If this was decided in haste, open your organization settings and click "Cancel deletion":
https://onevoice.app/business

After %s all organization data is erased permanently.

Best regards,
the OneVoice team
`, date, name, date)
	}
	return fmt.Sprintf(`Здравствуйте!

Через 7 дней (%s) организация «%s» в OneVoice будет удалена без возможности восстановления.

Если это решение принято в спешке — откройте настройки организации и нажмите «Отменить удаление»:
https://onevoice.app/business

После %s все данные организации удалятся безвозвратно.

С уважением,
команда OneVoice
`, date, name, date)
}

// BusinessDeletionT7WarningHTML is the HTML variant, in the owner's locale.
// Inline Linen palette per the email-rendering convention.
func BusinessDeletionT7WarningHTML(locale, name string, deletionAt time.Time) string {
	date := fmtDeletionDate(locale, deletionAt)
	if localeEN(locale) {
		return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><body style="background:#F5E9D9;color:#2C2520;font-family:Arial,sans-serif;padding:32px;">
<h1 style="font-size:24px;font-weight:500;margin:0 0 16px 0;color:#B0432F;">Organization deletion — 7 days left</h1>
<p style="font-size:15px;line-height:1.55;margin:0 0 16px 0;">
Hello! In 7 days (<strong>%s</strong>) the organization <strong>%s</strong> in OneVoice will be deleted with no way to restore it.
</p>
<p style="font-size:15px;line-height:1.55;margin:0 0 24px 0;">
If this was decided in haste, open your organization settings and cancel the deletion.
</p>
<a href="https://onevoice.app/business" style="display:inline-block;background:#D89B5A;color:#2C2520;padding:12px 24px;border-radius:10px;text-decoration:none;font-weight:500;">Open organization settings</a>
<p style="font-size:13px;color:#6B5C50;margin:32px 0 0 0;">
After %s all organization data is erased permanently.<br>Best regards, the OneVoice team
</p>
</body></html>
`, date, name, date)
	}
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
`, date, name, date)
}

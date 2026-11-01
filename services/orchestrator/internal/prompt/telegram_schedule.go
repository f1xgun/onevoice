package prompt

// Future intent routes through the approval-preserving API queue, never an immediate send.
// Keep this routing instruction in the business block, outside the cached prefix.
const telegramSchedulingRulesEn = "\nFor a Telegram post requested for a future time, include scheduled_at as a future RFC3339 timestamp on the existing send tool. Never omit the time or send it immediately. The user must manually approve the text/photo and scheduled time. Only report scheduling after the API returns a confirmed scheduled post record.\n"
const telegramSchedulingRulesRu = "\nЕсли пост в Telegram нужен в будущем, передай scheduled_at — будущую дату и время в RFC3339 — в существующий инструмент отправки. Не пропускай время и не отправляй пост немедленно. Пользователь должен вручную одобрить текст или фото и время. Сообщай об успешном планировании только после подтверждения записи от API.\n"

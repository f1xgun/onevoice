package prompt

// Until the approved-chat bridge exists, future intent must not become a send now.
// Keep this routing instruction in the business block, outside the cached prefix.
const telegramSchedulingRulesEn = "\nFor a Telegram post requested for a future time, never call the immediate send tools or publish it now. Chat scheduling is not supported yet. Present the draft and clearly explain that it has not been scheduled. Do not claim that a timer or scheduled post exists.\n"
const telegramSchedulingRulesRu = "\nЕсли пост в Telegram нужен в будущем, не вызывай инструменты немедленной отправки и не публикуй его сейчас. Отложенная публикация через чат пока не поддерживается. Покажи текст черновика и прямо объясни, что публикация не запланирована. Не утверждай, что создан таймер или отложенный пост.\n"

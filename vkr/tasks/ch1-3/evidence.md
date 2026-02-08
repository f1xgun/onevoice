## Evidence: ch1-3

Формат блока:

- **утверждение**: ...
- **источник_1**: ...
- **источник_2**: ...
- **источник_3**: ...
- **дата/контекст**: ...
- **заметки**: ...

---

- **утверждение**: Мультиагентные (многоагентные) системы характеризуются распределённостью, автономностью агентов и возможностью специализации/кооперации для решения сложных задач.
- **источник_1**: Zhang X. et al. A Survey of Multi-AI Agent Collaboration: Theories, Technologies and Applications. Proceedings of DEAI '25, 2025. URL: https://dl.acm.org/doi/full/10.1145/3745238.3745531 (дата обращения: 2026-02-01).
- **источник_2**: docs/Тема для ВКР.txt — описывает систему кооперирующихся автономных агентов и агента-оркестратора как подход к задаче синхронизации цифрового присутствия. Локальный файл проекта.
- **источник_3**: Agent2Agent (A2A) Protocol Specification (Release Candidate v1.0) — вводная часть описывает экосистему независимых “opaque agent systems” и цели интероперабельности. URL: https://a2a-protocol.org/latest/specification/ (дата обращения: 2026-02-01).
- **дата/контекст**: Поддерживается как современными обзорными материалами по multi-agent collaboration, так и проектной постановкой задачи в ВКР.
- **заметки**: В тексте ВКР использовать термин “мультиагентная система (МАС)” и расшифровать при первом упоминании.

- **утверждение**: A2A (Agent2Agent) протокол позиционируется как открытый стандарт для взаимодействия независимых агентных систем и использует известные транспорт/форматы (HTTP, JSON-RPC 2.0, SSE).
- **источник_1**: A2A Protocol Specification — Guiding Principles: reuse HTTP, JSON-RPC 2.0, Server-Sent Events. URL: https://a2a-protocol.org/latest/specification/ (дата обращения: 2026-02-01).
- **источник_2**: A2A Protocol (official docs) — “open standard designed to enable seamless communication and collaboration between AI agents”. URL: https://a2a-protocol.org/latest/ (дата обращения: 2026-02-01).
- **источник_3**: JSON-RPC 2.0 Specification (официальный сайт): URL: https://www.jsonrpc.org/specification (дата обращения: 2026-02-01).
- **дата/контекст**: В 1.3 это позволяет обосновать выбор стандартизированного A2A-взаимодействия и снижение связности.
- **заметки**: Не смешивать A2A и MCP: A2A — “agent-to-agent”, MCP — “host/client/server для контекста и инструментов”.

- **утверждение**: MCP (Model Context Protocol) определяет стандартизированный способ интеграции LLM-приложений с внешними источниками данных и инструментами и использует JSON-RPC 2.0.
- **источник_1**: Model Context Protocol Specification 2025-11-25 — Overview/Key Details (JSON-RPC 2.0; Hosts/Clients/Servers). URL: https://modelcontextprotocol.io/specification/2025-11-25 (дата обращения: 2026-02-01).
- **источник_2**: A2A Protocol (What is A2A) — раздел “How does A2A work with MCP?” описывает MCP как комплементарный стандарт для agent-to-tool communication. URL: https://a2a-protocol.org/latest/ (дата обращения: 2026-02-01).
- **источник_3**: JSON-RPC 2.0 Specification (официальный сайт). URL: https://www.jsonrpc.org/specification (дата обращения: 2026-02-01).
- **дата/контекст**: Это используется в ВКР для выбора подхода “единый протокол для интеграций” и для описания реализации MCP-агентов.
- **заметки**: MCP-спецификация регулярно обновляется; в тексте фиксировать версию/дату спецификации.

- **утверждение**: Внешние платформы, необходимые для управления цифровым присутствием (например Telegram и Instagram), предоставляют официальные API для интеграции.
- **источник_1**: Telegram Bot API — “HTTP-based interface” и описание формата запросов/ответов. URL: https://core.telegram.org/bots/api (дата обращения: 2026-02-01).
- **источник_2**: Meta for Developers — Instagram Platform Overview (APIs для Instagram professional accounts; возможности публикации/модерации/инсайтов). URL: https://developers.facebook.com/docs/instagram-platform/overview/ (дата обращения: 2026-02-01).
- **источник_3**: Meta for Developers — Instagram Platform Reference (структура nodes/edges/host URLs). URL: https://developers.facebook.com/docs/instagram-platform/reference/ (дата обращения: 2026-02-01).
- **дата/контекст**: В 1.3 достаточно зафиксировать наличие официальных API как предпосылку к агентным интеграциям, детальная реализация будет в главе 2.
- **заметки**: Не обещать доступность/полноту API без учёта требований авторизации и ограничений (rate limits, access levels).

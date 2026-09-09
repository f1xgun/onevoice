# OneVoice — «Своим голосом»

Статус: выбранное направление от 6 сентября 2026 года. Документ задаёт целевую
систему; соответствие работающего продукта подтверждается отдельно приёмкой.

## Назначение и принципы

Владелец открывает OneVoice между клиентами, поручает текст, проверяет его и
разрешает внешнее действие. Интерфейс помогает различать эти этапы и продолжать
работу после отвлечения.

Характер системы: тёплый серый, зелёная глазурь, прямой набор Golos Text, подписи
на полях и постоянная граница решения. Композиция «Поручение → Черновик →
Ваше решение» остаётся правилом оформления процесса. Её вклад в узнаваемость
на людях не подтверждён.

По решению продуктового, аналитического и маркетингового совета от 8 сентября
2026 года работа над узнаваемостью снята с повестки разработки до 7 октября
2026 года. Внедрённая система сохраняется; бюджет разработки на усиление
композиции и поиск нового визуального отличия — ноль.

1. Главный объект — текст, который увидит клиент.
2. Подготовка, правка, согласование и выполнение имеют разные подписи.
3. Автор и площадка находятся рядом с текстом.
4. Плотность истории помогает искать; простор черновика помогает читать.
5. Цвет дополняет слова и значки.
6. Лендинг показывает тот же способ работы, что приложение.
7. Существующие цены, юридические тексты, гибридный вход и обязательное согласование сохраняются.

## Цветовые токены и роли поверхностей

HEX — нормативное значение. CSS хранит полноценный цвет; Tailwind использует
`var(--token)`, без `hsl()` вокруг него и без пересчёта в OKLCH/HSL.
Префикс собственных переменных — `--ov-`. Тёмная тема имеет собственные значения.

| Токен        | Светлая | Тёмная  | Назначение                  |
| ------------ | ------- | ------- | --------------------------- |
| paper        | #F5F4F0 | #202724 | Страница                    |
| paper-raised | #FFFFFF | #29332E | Документ, поле, перекрытие  |
| paper-sunken | #E7E9E3 | #35433B | Контекст и наведение        |
| ink          | #202724 | #F2F3ED | Основной текст              |
| ink-soft     | #58635D | #B9C3BA | Подписи и помощь            |
| line         | #CAD0C9 | #48564D | Декоративная структура      |
| control      | #77847C | #829389 | Значимая граница управления |
| brand        | #245C55 | #99C6BA | Действие, ссылка, фокус     |
| on-brand     | #FFFFFF | #202724 | Текст основной кнопки       |
| brand-hover  | #19463F | #B3D9CF | Наведение и нажатие         |
| brand-soft   | #E0EBE5 | #23453E | Выбранное и выделение       |
| success      | #41653B | #B6D3A0 | Подтверждённое выполнение   |
| warning      | #7A4C18 | #E6C186 | Препятствие или уточнение   |
| danger       | #A13C36 | #F1A39A | Ошибка или опасное действие |
| info         | #365F8A | #A7C8EB | Выполнение операции         |

`paper-well` равен `paper-sunken`; `ink-mid` равен `ink`; `ink-faint` равен
`ink-soft`; `line-soft` равен `line`. Все semantic-soft равны `paper-raised`.
На мягком выделении используется `ink`.

Семантические алиасы: background → paper; foreground → ink; card/popover →
paper-raised; primary → brand; primary-foreground → on-brand; secondary/muted →
paper-sunken; muted-foreground → ink-soft; accent → brand-soft;
accent-foreground → ink; input → control; border → line; ring → brand;
destructive → danger. Текст залитого destructive — белый в светлой теме и
#202724 в тёмной. Прикладная опасная кнопка по умолчанию контурная.

Старые `ov-accent` и `ochre` сохраняются только для совместимости. Новые места
используют `brand`; новые имена `ochre` вне слоя совместимости запрещены.
Не ослаблять полезный текст через opacity. `line` нельзя использовать как
единственный видимый признак поля. Opacity-модификаторы полных цветовых переменных
нельзя применять без проверки с Tailwind 3; для состояния нужен явный токен.
Цвета графиков сохраняют совместимость, но не гарантируют различимость серий:
графики проверяются отдельно с подписями.

## Переключатель темы: действующий контракт

Доступны три состояния `system` / `light` / `dark`. Переключатель
[ThemeSwitcher](../services/frontend/components/design-system/ThemeSwitcher.tsx)
получает состояние через ThemeProvider. Отсутствующее или неизвестное значение
cookie `NEXT_THEME` трактуется как `system` в
[lib/theme.ts](../services/frontend/lib/theme.ts).

[useThemeSwitcher](../services/frontend/hooks/useThemeSwitcher.ts) отправляет
`POST /api/theme` с JSON `{ "theme": "system" }` (либо `light`, либо `dark`).
[Обработчик](../services/frontend/app/api/theme/route.ts) проверяет значение,
возвращает 204 и сохраняет cookie на год, с `path=/`, `sameSite=lax`,
`httpOnly=false`, `secure` в production. Некорректные данные дают 400,
ограничение частоты — 429. После успеха вызывается `router.refresh()`; при
ошибке выбор возвращается к предыдущему и показывается локализованное сообщение.
Параллельное сохранение блокируется.

[Корневой layout](../services/frontend/app/layout.tsx) читает cookie на сервере.
Он ставит `light` или `dark` на `<html>` вместе с классами шрифтов и прокрутки;
при `system` не ставит ни одного класса темы. Поэтому начальный HTML уже
соответствует сохранённому выбору: нет мигания из-за клиентского определения
темы и не нужен inline-скрипт в head.

Системная тема работает через `@media (prefers-color-scheme: dark)` с селектором
`:root:not(.light)`. Явная светлая тема исключена из медиа-условия; явная тёмная
действует через `.dark` независимо от ОС. При `system` смена настройки ОС
обрабатывается CSS без сохранения нового значения cookie.

В [tailwind.config.ts](../services/frontend/tailwind.config.ts) реально используется
вариант, покрывающий класс **и** медиа-условие:

```ts
darkMode: [
  'variant',
  [
    '&:where(.dark, .dark *)',
    '@media (prefers-color-scheme: dark) { &:where(:root:not(.light), :root:not(.light) *) }',
  ],
],
```

Это необходимое отличие от исходной нормативной конфигурации `darkMode: ['class']`
в приложении ниже: её нельзя копировать поверх действующего варианта, иначе
`dark:*` перестанут следовать системной теме.

Тёмные токены в [globals.css](../services/frontend/app/globals.css) объявлены
**дважды**: в `.dark` и в `:root:not(.light)` внутри медиа-условия. При добавлении
или изменении тёмного токена обновлять оба объявления, включая имя и значение.
Алиасы объявляются на обоих корнях `:root, .dark`, чтобы вычисляться в нужной теме.
[theme-styles.test.ts](../services/frontend/lib/theme-styles.test.ts) сравнивает
имена и значения всех деклараций двух блоков и проверяет скомпилированный Tailwind
для шести сочетаний выбора и настройки ОС. Дополнительные регрессии:
[серверный layout](../services/frontend/__tests__/theme-layout.test.tsx),
[POST и cookie](../services/frontend/app/api/theme/route.test.ts),
[переключатель](../services/frontend/components/design-system/ThemeSwitcher.test.tsx).

## Типографика

Golos Text: 400 для чтения, 500 для действий, 600 для заголовков.
JetBrains Mono 400 — только технические данные и клавиши. Третьей семьи нет.
Шрифты подключаются через `next/font/google` с latin/cyrillic; Mono не
предзагружается. Файлы обслуживает приложение: Google Fonts нужны при сборке,
рантайм-CDN браузеру не нужен. Метаданные, провайдеры и `lang={locale}` сохраняются.

| Роль                      | Телефон | Компьютер | Tailwind                        |
| ------------------------- | ------- | --------- | ------------------------------- |
| Герой                     | 36/40   | 56/60     | text-hero md:text-hero-lg       |
| Раздел лендинга           | 28/34   | 36/42     | text-section md:text-section-lg |
| Заголовок приложения      | 24/30   | 24/30     | text-page-title                 |
| Заголовок документа       | 20/28   | 20/28     | text-document-title             |
| Черновик, сообщение, ввод | 16/25   | 16/25     | text-reading                    |
| Строка и кнопка           | 16/22   | 16/22     | text-action                     |
| Подпись и навигация       | 14/20   | 14/20     | text-meta                       |
| Технические подробности   | 13/18   | 13/18     | text-technical                  |
| Тарифная сумма            | 32/38   | 32/38     | text-price                      |

Размеры реализуются в rem. Ширина текста — до `66ch`; на телефоне используется
доступная ширина. Отрицательный tracking допустим только у крупных заголовков.
Не использовать рекламный моноширинный капс, искусственный курсив и
неподтверждённые OpenType-настройки. Проверять Ёё, Йй, Щщ, ₽, № и длинные
переводы. Табличные цифры включать для сравнения после проверки глифов.
Заголовки используют тот же sans через `font-display`.

## Геометрия и расстояния

Радиусы: 4/6/8/12 px. Поле и кнопка — 6, документ — 8, диалог — 12.
Полное скругление сохраняется у аватаров и функциональных переключателей.

Шкала отступов: 4/8/12/16/24/32/48/64/80 px. Лендинг до 1120 px; поля 20/40 px,
секции 48/80 px. Приложение: поля 16/32 px, списки до 1040 px. Строка истории
от 64 px и растёт с содержимым. Документ имеет внутренние поля 16/24 px.

Тень только у перекрытий: `0 8px 24px rgb(0 0 0 / 0.12)`, в тёмной теме
прозрачность 0.32. Затемнение: чёрный с прозрачностью 0.48/0.64. Страницы,
тарифы, история и черновики плоские.

## Фирменная композиция

Поручение → черновик → ваше решение → фактический результат.

На компьютере подпись занимает колонку 96 px, текст — оставшуюся ширину.
На телефоне подпись стоит сверху. Документ показывает тип текста, площадку,
адресата, редактируемое содержание и строку решения.

Перед «Ваше решение» расположены две горизонтальные линии длиной 24 и 40 px
с промежутком 3 px. `DecisionMark` использует два span `h-px w-6` / `w-10`,
`bg-brand`, `gap-[3px]`, `aria-hidden`. Мотив обозначает границу решения
владельца, не используется у тарифов, навигации и случайных заголовков.

## Компоненты и ограничения публичного API

Составные компоненты размещаются в `services/frontend/components/design-system/`
или прикладном модуле, **снаружи** `components/ui/*`. Generated-примитивы не
редактировать, не копировать и не регенерировать в рамках визуальной миграции.
Использовать только экспортированные компоненты и их типизированные публичные
параметры: `className`, поддерживаемые `classNames` / `components`, `variant`,
`size`, `asChild`, `ref`. Не считать, что каждый примитив поддерживает все эти
параметры: сначала проверить его типы. Не обходить ограничения через `any`.
Не вводить глобальную перекраску всех button или Radix-порталов.

**Кнопки.** Использовать `ActionButton` и `actionButtonVariants` вместо прикладных
импортов Button/buttonVariants из ui. Обёртка сохраняет ref/asChild и действующие
варианты. Основная — brand/on-brand; вторичная — raised/ink/control; опасная —
raised/danger/control. Минимальная область 44×44 px; высота растёт с текстом.
Одна основная кнопка на рабочую поверхность. Ссылки подчёркнуты. Disabled —
paper-sunken/ink-soft/control без цветного hover и без ослабления opacity.
`aria-disabled` сообщает состояние, но само по себе не блокирует действие:
для запрета использовать контракт `disabled`. Для иконки нужны доступное имя
и размер `icon`. `asChild` требует одного совместимого дочернего элемента;
не вкладывать кнопку в кнопку или ссылку в ссылку.

Общие классы: `min-h-11 h-auto whitespace-normal rounded-md px-4 py-2.5
text-action tracking-normal disabled:opacity-100 motion-reduce:transition-none`.
Основная: `bg-brand text-brand-foreground border-brand hover:bg-brand-hover
hover:text-brand-foreground hover:border-brand-hover active:bg-brand-hover`.
Вторичная: `bg-paper-raised text-ink border-control hover:bg-paper-sunken`.
Опасная: `bg-paper-raised text-danger border-control hover:bg-paper-sunken`.
Иконка: `min-h-11 min-w-11 p-2.5`. Проверять итоговый CSS, включая наследуемые
hardcoded hover и пары accent/ink из примитива: один `cn`/`twMerge` не доказывает
правильный каскад. Публичный className не гарантирует переопределение обязательных
классов обёртки; порядок слияния проверяется по реализации и CSS.

**Сообщения.** Автор и текст без рамки вокруг каждой реплики. Контекст владельца
может иметь вторичную подложку. Проверяемый материал получает одну `DraftSurface`:
существующий Card с `bg-card rounded-lg border-line shadow-none`, текстовая область
`min-w-0`. Markdown наследует читаемый размер и цвета обеих тем.

**История.** Название, время создания, меню. Без повторяющейся иконки чата и
отдельной рамки у каждой строки. Меню доступно касанием и клавиатурой. Ручные
названия сохраняются. Статусы согласования и последняя активность показываются
только при наличии данных.

**Навигация.** Видимые подписи и вторичные Lucide-значки 18–20 px. Выбранность
обозначается фоном, насыщенностью и `aria-current`. На корневом `/chat` история
не дублируется в ProjectPane.

**Формы.** Постоянная подпись, поле 16/25, граница control, помощь и ошибка рядом.
Использовать react-hook-form и zod. «Сохранено» появляется после успешного ответа.
Не скрывать единственную информацию о результате в тосте.

**Диалоги.** `AppDialog` использует установленный Radix Content и экспортированные
Portal/Overlay/Close без второго вложенного портала и двойного overlay. Overlay —
`bg-overlay`; content — `bg-card border-control rounded-xl shadow-overlay`,
`w-[calc(100%-2rem)] max-w-[30rem] max-h-[calc(100dvh-2rem)]`. Сетка содержит
заголовок, прокручиваемый средний div и действия. Передавать локализованные
DialogTitle и DialogDescription; закрытие локализовано обёрткой и имеет 44×44 px.
Сохранять Escape, focus trap и возврат фокуса. Действия достижимы при экранной
клавиатуре. Длинное согласование остаётся частью чата. Если API generated-примитива
недостаточен, собрать небольшую прикладную композицию из уже установленного Radix
с сохранением доступного поведения.

**Состояния.** `StatusLine` принимает явную роль `status` или `alert`, локализованный
`text`, Lucide `icon` и `tone`; не вычисляет серверное состояние. «Проверьте текст» —
ожидается решение; «Отправляется» — операция выполняется; «Опубликовано» — есть
подтверждение; «Не удалось отправить» — подтверждённый сбой; «Не удалось проверить
отправку» — исход неизвестен. Цвет всегда сопровождается словами и значком.
Согласование не называть публикацией, неизвестный исход не окрашивать в успех.
Не предлагать опасный повтор без существующей защиты от дублирования.

**Пустота.** Показывать только после успешного пустого ответа. Ошибка загрузки
имеет отдельное сообщение и действие восстановления. Стартовая подсказка
опирается на доступные возможности.

## Как добавить цвет

Сначала проверить существующие роли. Если новой роли действительно нет, описать
назначение и конкретные состояния, выбрать значения обеих тем и вычислить
контраст на каждой разрешённой поверхности. Добавить переменные globals.css
(тёмную — в оба объявления), семантическое имя Tailwind и строку таблицы.
Проверить реальный CSS с hover, focus и прозрачностью. Не добавлять HEX
непосредственно в JSX и не вводить цвет только ради одной секции.

## Как добавить компонент

Сначала проверить components/ui и существующие прикладные композиции. Собрать
компонент вне ui с типизированными props, function declaration и Tailwind-классами.
Серверный компонент по умолчанию; логику вынести в hook при необходимости.
Никаких HEX в разметке, inline style или изменений ui/\*.

Определить состояния загрузки, ошибки, пустоты, disabled и клавиатурного фокуса.
Новые строки добавить в обе локали ru/en: «организация», «ИИ». Проверить мобильную
и настольную ширину, обе темы и длинные данные. Тестировать поведение, которое
может потерять правку или изменить внешнее действие; не писать тест ради одного класса.

Пример новой серверной композиции: только представление переданного черновика,
без имитации сохранения, согласования или отправки. Все подписи передаёт
локализованный вызывающий компонент; `id` должен быть уникален на странице.

```tsx
import { DraftSurface } from "@/components/design-system/DraftSurface";

interface DraftPreviewProps {
  id: string;
  title: string;
  platformLabel: string;
  recipientLabel: string;
  text: string;
}

export function DraftPreview({
  id,
  title,
  platformLabel,
  recipientLabel,
  text,
}: DraftPreviewProps) {
  return (
    <section aria-labelledby={id} data-ov-motion className="min-w-0 text-ink">
      <DraftSurface>
        <h2 id={id} className="font-display text-document-title">
          {title}
        </h2>
        <div className="mt-3 grid min-w-0 gap-2 text-meta text-ink-soft sm:grid-cols-[96px_minmax(0,1fr)]">
          <span>{platformLabel}</span>
          <span>{recipientLabel}</span>
        </div>
        <p className="mt-4 max-w-[66ch] whitespace-pre-wrap break-words text-reading">
          {text}
        </p>
      </DraftSurface>
    </section>
  );
}
```

Для интерактивного варианта взять ActionButton и существующий контракт обработки
решения; не объявлять успех по клику. Ошибки, сохранение правки и согласование
проверять поведенческими Vitest-тестами.

## Лендинг

Последовательность: шапка → обещание и входы → пример → два сценария с площадками →
тарифы → вопросы → заявка и документы.

Заголовок: «Посты и ответы — с вашим последним словом».

Пример — доступный HTML на демонстрационных данных, использующий DraftSurface.
Статический пример не содержит ложных кнопок. Отсутствуют браузерные точки,
адресная строка и вымышленное время работы.

Основное действие — лист ожидания. Регистрация заметна и сохраняет действующее
объяснение доступности. Мобильная панель сохраняет согласованную логику и не
перекрывает форму, фокус и клавиатуру. Цены, условия, юридические тексты и тарифные
CTA неизменны. До миграции сопоставлять рабочую ревизию с LIVE и фиксировать
`data-cta`, `hero`/`waitlist` и условия мобильной панели; старый LIVE не является
доказательством состояния новой ревизии.

## Делать и избегать

| Делать                                               | Избегать                                                          |
| ---------------------------------------------------- | ----------------------------------------------------------------- |
| Оформлять поручение, документ и результат по-разному | Одинаковой карточки для любой сущности                            |
| Показывать конкретную правку текста                  | Повторять универсальные обещания пользы                           |
| Использовать один прямой шрифтовой голос             | Сочетания гротеска, курсивной антиквы и рекламного Mono           |
| Связывать акцент с решением                          | Окрашивать им все поверхности и предупреждения                    |
| Показывать настоящий сценарий                        | Парящего псевдоокна с декоративными счётчиками                    |
| Подписывать разделы и площадки                       | Буквенных марок и навигации только из значков                     |
| Подбирать интервалы под содержание                   | Одинаковой разреженной секции для каждого аргумента               |
| Сохранять полезный текст и входы                     | Скрывать условия ради красивого первого экрана                    |
| Оставлять пустоту там, где нет данных                | Выдуманных задач, отзывов, результатов и иллюстрационных заглушек |

Эти признаки описывают восприятие композиции, а не доказывают генерацию сайта.
Тёплый фон и карточка сами по себе не являются ошибкой.

## Доступность и движение

Цель — WCAG AA в обеих темах. Обычный текст ≥4,5:1; значимые контуры и индикаторы
≥3:1. Фокус 2 px с отступом 2 px. 44×44 px — стандарт удобства проекта.

Проверять ru/en, 375/1440 px, дополнительно 320 CSS px, 200% текста,
пользовательские интервалы, клавиатуру и диктор. Ошибки связаны с полями;
aria-live сообщает о значимых результатах, а не о каждом токене ответа.
Закреплённые элементы не скрывают фокус. В оболочках задавать адаптивные
Tailwind scroll-padding с запасом для шапки и мобильной панели; текущий html
использует `scroll-pb-32 scroll-pt-24 md:scroll-pb-8 md:scroll-pt-28`.

Переходы цвета и прозрачности 120–160 мс. Нет искусственного ожидания, анимации
заголовков, движения фона и масштабирования диалога. Reduced motion отключает
прикладные animation/transition и smooth scroll в области `data-ov-motion`,
включая потомков и псевдоэлементы; новые композиции используют этот атрибут.
Новое сообщение не отнимает позицию чтения. `[data-highlight='true']` получает
статический accent-soft на срок существующего хука без onevoice-flash.
Selection сохраняется через accent-soft/ink.

Переключатели темы и языка собираются через `AppearanceControls`: в публичной
шапке — два элемента 44×44 px, в меню — полноширинные строки с общей осью
иконок и подписей. На десктопе нижние действия закреплены, прокручивается
средняя часть навигации. `template.tsx` публичной и рабочей групп добавляет
проявление страницы за 160 мс, сохраняя оболочку навигации.

`PlatformIcon` использует локальные SVG из `public/platforms`; происхождение
указано в README каталога. Рядом остаётся название площадки, а значок и текст
состояния подключения отображаются отдельно. Для неизвестной площадки
используется нейтральный глобус.

## Граница выпуска и приёмка

Серверная история версий, источники генерации, новые статусы списка и
персистентность правок после перезагрузки не входят автоматически в визуальную
миграцию. Их нельзя имитировать. Существующие права доступа, чат-модель,
согласование и защита от повторного выполнения сохраняются.

Не меняются Free/Pro/Enterprise, суммы, «за локацию», фиксация беты; юридические
тексты и версии в content/legal, pkg/legalconfig, lib/legal; `LANDING_ENTRY_MODE`,
входы `/login`, `/register`, лист ожидания и `data-cta`; generated ui/\*; lockfile
и версии зависимостей. Новые зависимости и переменные окружения не нужны.

Техническая основа определяется package.json и lockfile: здесь Next.js 15.3.9,
Tailwind объявлен как `^3.4.1`, lockfile разрешает 3.4.19. Старое упоминание
Next.js 14 не повод делать downgrade. Шрифты обслуживает приложение.
Проверка юридических реквизитов в build сохраняется; при отсутствии значений
сообщать точную причину, не заполнять вымышленными данными.

Документ не является протоколом успешной итоговой приёмки. После завершения
визуальных изменений фиксировать ревизию, экран, данные, тему, локаль, ширину,
действия и результат для всех 375/1440 × light/dark × ru/en; отдельно 320 CSS px,
200% текста, интервалы, reduced motion, клавиатуру и диктор. Старый отчёт
[проверки основы](../services/frontend/docs/design-system-verification.md)
не заменяет итоговый прогон. В этой документной работе итоговая матрица —
**не проверено**.

Тест понятности: пять владельцев без подсказки находят текст, исправляют время
и объясняют, куда он отправится и произошло ли выполнение. Засчитывается только
участник, выполнивший все действия и правильно различивший согласование и
выполнение. Порог — минимум 4 из 5. Использовать безопасный сценарий без реального
внешнего действия; записывать наблюдения по каждому участнику, ошибки и помощь.
Участники для этой работы не предоставлены: результат — **не проверено**.

Узнаваемость проверяется отдельно: после знакомства человек сопоставляет
фрагмент черновика без логотипа с другим экраном OneVoice. Фиксировать варианты,
ответ и объяснение, не подсказывать цветом или названием продукта. Результат
этой работы — **не проверено**. Узнавание и понятность не являются статистическим
доказательством роста конверсии.

Итоговый отчёт должен перечислять изменения, выполненные проверки и ограничения,
отдельно подтверждать сохранность ui/\*, цен, юридических текстов и режима входа.
Каждая часть миграции требует собственной проверки перед продолжением.

## Основания

Материалы исходного направления:
[Linear](https://linear.app/now/behind-the-latest-design-refresh),
[Buffer](https://buffer.com/resources/introducing-a-calmer-more-flexible-buffer/),
[Intercom Copilot](https://www.intercom.com/help/en/articles/8587194-how-to-use-copilot).
Для прерываемой работы полезен принцип сохранения незавершённого заметным у
[Wazzup](https://wazzup24.ru/help/how-to-use/unanswered-counter/); внедрение статуса
в историю OneVoice требует реальных данных. Выбранные цвет, геометрия и гарнитура —
проектное решение. Подключение шрифтов описано в
[документации Next.js](https://nextjs.org/docs/app/api-reference/components/font).

## Приложение A. Нормативный CSS

Ниже исходный нормативный блок с точными HEX и алиасами. Для действующего
`system` дополнительно требуется идентичный блок тёмных деклараций в
`@media (prefers-color-scheme: dark) { :root:not(.light) { … } }`, как описано
в разделе о переключателе; не удалять его при обновлении токенов.

<!-- prettier-ignore -->
```css
@layer base {
  :root {
    color-scheme: light;
    --ov-paper: #F5F4F0;
    --ov-paper-raised: #FFFFFF;
    --ov-paper-sunken: #E7E9E3;
    --ov-ink: #202724;
    --ov-ink-soft: #58635D;
    --ov-line: #CAD0C9;
    --ov-control: #77847C;
    --ov-brand: #245C55;
    --ov-on-brand: #FFFFFF;
    --ov-brand-hover: #19463F;
    --ov-brand-soft: #E0EBE5;
    --ov-success: #41653B;
    --ov-warning: #7A4C18;
    --ov-danger: #A13C36;
    --ov-on-danger: #FFFFFF;
    --ov-info: #365F8A;
    --ov-overlay: rgb(0 0 0 / 0.48);
    --ov-shadow-overlay: 0 8px 24px rgb(0 0 0 / 0.12);
    --ov-radius-sm: 4px;
    --ov-radius-md: 6px;
    --ov-radius-lg: 8px;
    --ov-radius-xl: 12px;
  }
  .dark {
    color-scheme: dark;
    --ov-paper: #202724;
    --ov-paper-raised: #29332E;
    --ov-paper-sunken: #35433B;
    --ov-ink: #F2F3ED;
    --ov-ink-soft: #B9C3BA;
    --ov-line: #48564D;
    --ov-control: #829389;
    --ov-brand: #99C6BA;
    --ov-on-brand: #202724;
    --ov-brand-hover: #B3D9CF;
    --ov-brand-soft: #23453E;
    --ov-success: #B6D3A0;
    --ov-warning: #E6C186;
    --ov-danger: #F1A39A;
    --ov-on-danger: #202724;
    --ov-info: #A7C8EB;
    --ov-overlay: rgb(0 0 0 / 0.64);
    --ov-shadow-overlay: 0 8px 24px rgb(0 0 0 / 0.32);
  }
  :root,
  .dark {
    --ov-paper-well: var(--ov-paper-sunken);
    --ov-ink-mid: var(--ov-ink);
    --ov-ink-faint: var(--ov-ink-soft);
    --ov-line-soft: var(--ov-line);
    --ov-accent: var(--ov-brand);
    --ov-accent-deep: var(--ov-brand-hover);
    --ov-accent-soft: var(--ov-brand-soft);
    --ov-accent-ink: var(--ov-ink);
    --ov-success-soft: var(--ov-paper-raised);
    --ov-warning-soft: var(--ov-paper-raised);
    --ov-warning-ink: var(--ov-warning);
    --ov-danger-soft: var(--ov-paper-raised);
    --ov-info-soft: var(--ov-paper-raised);
    --ov-shadow-1: none;
    --ov-shadow-2: var(--ov-shadow-overlay);
    --ov-shadow-3: var(--ov-shadow-overlay);
    --background: var(--ov-paper);
    --foreground: var(--ov-ink);
    --card: var(--ov-paper-raised);
    --card-foreground: var(--ov-ink);
    --popover: var(--ov-paper-raised);
    --popover-foreground: var(--ov-ink);
    --primary: var(--ov-brand);
    --primary-foreground: var(--ov-on-brand);
    --secondary: var(--ov-paper-sunken);
    --secondary-foreground: var(--ov-ink);
    --muted: var(--ov-paper-sunken);
    --muted-foreground: var(--ov-ink-soft);
    --accent: var(--ov-brand-soft);
    --accent-foreground: var(--ov-ink);
    --destructive: var(--ov-danger);
    --destructive-foreground: var(--ov-on-danger);
    --border: var(--ov-line);
    --input: var(--ov-control);
    --ring: var(--ov-brand);
    --chart-1: var(--ov-brand);
    --chart-2: var(--ov-success);
    --chart-3: var(--ov-warning);
    --chart-4: var(--ov-info);
    --chart-5: var(--ov-ink-soft);
    --radius: var(--ov-radius-md);
  }
}
```

Дополнительные правила базового CSS (внутри существующего `@layer base`):

```css
html {
  font-family: var(--font-sans), ui-sans-serif, system-ui, sans-serif;
  font-feature-settings: normal;
}
code,
kbd,
samp,
pre,
.font-mono {
  font-family: var(--font-mono), ui-monospace, monospace;
}
::selection {
  background: var(--ov-accent-soft);
  color: var(--ov-ink);
}
```

Вне базового слоя:

```css
[data-highlight="true"] {
  background-color: var(--ov-accent-soft);
}
@media (prefers-reduced-motion: reduce) {
  [data-ov-motion],
  [data-ov-motion] *,
  [data-ov-motion]::before,
  [data-ov-motion]::after,
  [data-ov-motion] *::before,
  [data-ov-motion] *::after {
    animation: none !important;
    transition: none !important;
    scroll-behavior: auto !important;
  }
}
```

Вводный комментарий описывает «Своим голосом» и полные HEX; устаревший пример
Linen с `hsl(var())` и анимация onevoice-flash удаляются.

## Приложение B. Нормативная конфигурация Tailwind

Это дополнения/замены **внутри одного** `theme.extend`, не второй объект colors.
Ниже сохранены существующие семантические цвета, content и плагины. Исходное
`darkMode: ['class']` приведено для точности спецификации; действующее приложение
сохраняет расширенный `variant` из раздела о теме. Значения токенов, типографики,
радиусов и теней от этого не меняются. `ochre` — только устаревающий алиас.

```ts
import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: ["class"],
  content: [
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        background: "var(--background)",
        foreground: "var(--foreground)",
        card: {
          DEFAULT: "var(--card)",
          foreground: "var(--card-foreground)",
        },
        popover: {
          DEFAULT: "var(--popover)",
          foreground: "var(--popover-foreground)",
        },
        primary: {
          DEFAULT: "var(--primary)",
          foreground: "var(--primary-foreground)",
        },
        secondary: {
          DEFAULT: "var(--secondary)",
          foreground: "var(--secondary-foreground)",
        },
        muted: {
          DEFAULT: "var(--muted)",
          foreground: "var(--muted-foreground)",
        },
        accent: {
          DEFAULT: "var(--accent)",
          foreground: "var(--accent-foreground)",
        },
        destructive: {
          DEFAULT: "var(--destructive)",
          foreground: "var(--destructive-foreground)",
        },
        border: "var(--border)",
        input: "var(--input)",
        ring: "var(--ring)",
        chart: {
          "1": "var(--chart-1)",
          "2": "var(--chart-2)",
          "3": "var(--chart-3)",
          "4": "var(--chart-4)",
          "5": "var(--chart-5)",
        },

        paper: {
          DEFAULT: "var(--ov-paper)",
          raised: "var(--ov-paper-raised)",
          sunken: "var(--ov-paper-sunken)",
          well: "var(--ov-paper-well)",
        },
        ink: {
          DEFAULT: "var(--ov-ink)",
          mid: "var(--ov-ink-mid)",
          soft: "var(--ov-ink-soft)",
          faint: "var(--ov-ink-faint)",
        },
        line: {
          DEFAULT: "var(--ov-line)",
          soft: "var(--ov-line-soft)",
        },
        brand: {
          DEFAULT: "var(--ov-brand)",
          foreground: "var(--ov-on-brand)",
          hover: "var(--ov-brand-hover)",
          soft: "var(--ov-brand-soft)",
        },
        control: "var(--ov-control)",
        overlay: "var(--ov-overlay)",
        ochre: {
          DEFAULT: "var(--ov-accent)",
          deep: "var(--ov-accent-deep)",
          soft: "var(--ov-accent-soft)",
          ink: "var(--ov-accent-ink)",
        },
        success: {
          DEFAULT: "var(--ov-success)",
          soft: "var(--ov-success-soft)",
        },
        warning: {
          DEFAULT: "var(--ov-warning)",
          soft: "var(--ov-warning-soft)",
          ink: "var(--ov-warning-ink)",
        },
        danger: {
          DEFAULT: "var(--ov-danger)",
          soft: "var(--ov-danger-soft)",
        },
        info: {
          DEFAULT: "var(--ov-info)",
          soft: "var(--ov-info-soft)",
        },
      },
      fontFamily: {
        sans: ["var(--font-sans)", "ui-sans-serif", "system-ui", "sans-serif"],
        display: [
          "var(--font-sans)",
          "ui-sans-serif",
          "system-ui",
          "sans-serif",
        ],
        mono: ["var(--font-mono)", "ui-monospace", "monospace"],
      },
      fontSize: {
        hero: [
          "2.25rem",
          {
            lineHeight: "2.5rem",
            letterSpacing: "-0.025em",
            fontWeight: "600",
          },
        ],
        "hero-lg": [
          "3.5rem",
          {
            lineHeight: "3.75rem",
            letterSpacing: "-0.025em",
            fontWeight: "600",
          },
        ],
        section: [
          "1.75rem",
          {
            lineHeight: "2.125rem",
            letterSpacing: "-0.015em",
            fontWeight: "600",
          },
        ],
        "section-lg": [
          "2.25rem",
          {
            lineHeight: "2.625rem",
            letterSpacing: "-0.015em",
            fontWeight: "600",
          },
        ],
        "page-title": ["1.5rem", { lineHeight: "1.875rem", fontWeight: "600" }],
        "document-title": [
          "1.25rem",
          { lineHeight: "1.75rem", fontWeight: "600" },
        ],
        reading: ["1rem", { lineHeight: "1.5625rem" }],
        action: ["1rem", { lineHeight: "1.375rem", fontWeight: "500" }],
        meta: ["0.875rem", { lineHeight: "1.25rem" }],
        technical: ["0.8125rem", { lineHeight: "1.125rem" }],
        price: ["2rem", { lineHeight: "2.375rem", fontWeight: "500" }],
      },
      borderRadius: {
        sm: "var(--ov-radius-sm)",
        md: "var(--ov-radius-md)",
        lg: "var(--ov-radius-lg)",
        xl: "var(--ov-radius-xl)",
      },
      boxShadow: {
        overlay: "var(--ov-shadow-overlay)",
        "ov-1": "var(--ov-shadow-1)",
        "ov-2": "var(--ov-shadow-2)",
        "ov-3": "var(--ov-shadow-3)",
      },
    },
  },
  plugins: [require("tailwindcss-animate"), require("@tailwindcss/typography")],
};
export default config;
```

## Приложение C. Загрузчики шрифтов

В `app/layout.tsx` сохраняются метаданные, провайдеры, `lang={locale}` и классы
`${sans.variable} ${mono.variable}` на html вместе с серверным классом темы.
Удаляется комментарий о временной замене Mona Sans. Третьего загрузчика нет.

```tsx
import { Golos_Text, JetBrains_Mono } from "next/font/google";

const sans = Golos_Text({
  subsets: ["latin", "cyrillic"],
  weight: ["400", "500", "600"],
  variable: "--font-sans",
  display: "swap",
});
const mono = JetBrains_Mono({
  subsets: ["latin", "cyrillic"],
  weight: "400",
  variable: "--font-mono",
  display: "swap",
  preload: false,
});
```

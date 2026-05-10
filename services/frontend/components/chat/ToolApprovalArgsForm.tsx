'use client';

import { memo, useId } from 'react';
import { Lock } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';

import { evaluateEditGate } from './toolApprovalGate';

// Keys whose typical content is multi-paragraph copy — render as <Textarea>
// instead of a single-line <Input>. Inferred from the orchestrator's tool
// registrations (wire/tools_*.go); operators write product copy, captions,
// descriptions and free-form schedules in these fields.
const LONG_TEXT_KEYS = new Set([
  'text',
  'caption',
  'description',
  'hours',
  'body',
  'message',
  'content',
]);

// String length at which an otherwise-short key (not in LONG_TEXT_KEYS) still
// promotes to a <Textarea> — a generic guard so the LLM occasionally proposing
// a verbose value for a normally-short field still gets a usable editor.
const TEXTAREA_PROMOTION_THRESHOLD = 80;

// Heuristic row sizing for the textarea: roughly one row per
// TEXTAREA_CHARS_PER_ROW chars, clamped between MIN and MAX rows so the
// control never collapses to a one-liner and never grows to fill the viewport.
const TEXTAREA_CHARS_PER_ROW = 60;
const TEXTAREA_MIN_ROWS = 3;
const TEXTAREA_MAX_ROWS = 8;

export interface ToolApprovalArgsFormProps {
  /** Persisted server args — the source of truth for what the LLM proposed. */
  args: Record<string, unknown>;
  /** Top-level scalar overrides the user has staged so far. */
  editedArgs: Record<string, string | number | boolean>;
  /** Per-tool whitelist from the SSE event's `editable_fields`. */
  editableFields: string[];
  /** True when the operator has picked "Изменить" — turns on editable controls. */
  editable: boolean;
  /** True while the resolve is in flight (parent disables every interactive control). */
  disabled: boolean;
  /** Called with `(key, value)` for every accepted scalar edit. */
  onEdit: (key: string, value: string | number | boolean) => void;
}

interface FieldRow {
  key: string;
  /** Localized human-readable label. */
  label: string;
  /** Effective value: edited override (if any), else persisted. */
  value: unknown;
  /** Whether this field is in the per-tool whitelist. */
  isEditable: boolean;
}

/**
 * Form-based replacement for the JSON-tree editor. Renders one labelled
 * control per parameter so non-technical operators can review and edit a
 * pending tool call without parsing JSON.
 *
 * Edit gate: every change still flows through `evaluateEditGate` so the
 * server-side invariants (root-only, scalar-only, whitelist) hold from the
 * input layer down. Non-scalar / nested values can only appear in the locked
 * section (read-only) since the whitelist forbids them in writes anyway.
 */
export const ToolApprovalArgsForm = memo(function ToolApprovalArgsForm({
  args,
  editedArgs,
  editableFields,
  editable,
  disabled,
  onEdit,
}: ToolApprovalArgsFormProps) {
  const t = useTranslations('chat.toolApproval');
  const idPrefix = useId();

  const argKeys = Object.keys(args);
  if (argKeys.length === 0) {
    return <p className="text-xs text-muted-foreground">{t('noArgs')}</p>;
  }

  const editableSet = new Set(editableFields);
  const rows: FieldRow[] = argKeys.map((key) => ({
    key,
    label: resolveLabel(t, key),
    value: editedArgs[key] !== undefined ? editedArgs[key] : args[key],
    isEditable: editableSet.has(key),
  }));

  // Stable ordering: editable rows first (in the order declared by
  // `editableFields` so the registry-declared priority is honoured), then
  // every locked row in insertion order. Operators scan from the actionable
  // section into the locked context block.
  const editableRows = editableFields
    .filter((k) => k in args)
    .map((k) => rows.find((r) => r.key === k))
    .filter((r): r is FieldRow => r !== undefined);
  const lockedRows = rows.filter((r) => !editableSet.has(r.key));

  // When the operator has not picked Edit yet, the whole form renders as a
  // read-only context list with no section split — there is nothing to act
  // on, so a single uniform list is easier to scan.
  if (!editable) {
    return (
      <dl className="grid grid-cols-1 gap-y-2 text-sm sm:grid-cols-[max-content_1fr] sm:gap-x-3">
        {rows.map((row) => (
          <ReadOnlyRow key={row.key} row={row} t={t} />
        ))}
      </dl>
    );
  }

  return (
    <div className="space-y-4">
      {editableRows.length > 0 ? (
        <section className="space-y-3" aria-labelledby={`${idPrefix}-editable`}>
          <h4 id={`${idPrefix}-editable`} className="text-xs font-semibold text-ink-mid">
            {t('editableSectionHeading')}
          </h4>
          <div className="space-y-3">
            {editableRows.map((row) => (
              <EditableField
                key={row.key}
                row={row}
                idPrefix={idPrefix}
                disabled={disabled}
                editableFields={editableFields}
                onEdit={onEdit}
              />
            ))}
          </div>
        </section>
      ) : (
        <p className="text-xs text-muted-foreground">{t('noEditableFields')}</p>
      )}

      {lockedRows.length > 0 && (
        <section className="space-y-2" aria-labelledby={`${idPrefix}-locked`}>
          <h4
            id={`${idPrefix}-locked`}
            className="flex items-center gap-1.5 text-xs font-semibold text-ink-mid"
          >
            <Lock size={12} aria-hidden="true" />
            {t('lockedSectionHeading')}
          </h4>
          <dl className="grid grid-cols-1 gap-y-2 text-sm sm:grid-cols-[max-content_1fr] sm:gap-x-3">
            {lockedRows.map((row) => (
              <ReadOnlyRow key={row.key} row={row} t={t} />
            ))}
          </dl>
          <p className="text-xs text-muted-foreground">{t('lockedHint')}</p>
        </section>
      )}
    </div>
  );
});

interface EditableFieldProps {
  row: FieldRow;
  idPrefix: string;
  disabled: boolean;
  editableFields: string[];
  onEdit: (key: string, value: string | number | boolean) => void;
}

function EditableField({ row, idPrefix, disabled, editableFields, onEdit }: EditableFieldProps) {
  const id = `${idPrefix}-${row.key}`;
  const value = row.value;

  // Boolean → Switch with inline yes/no caption.
  if (typeof value === 'boolean') {
    return (
      <div className="flex items-center justify-between gap-3">
        <Label htmlFor={id} className="text-sm font-medium">
          {row.label}
        </Label>
        <Switch
          id={id}
          checked={value}
          disabled={disabled}
          onCheckedChange={(next) => commitEdit(row.key, next, editableFields, onEdit)}
        />
      </div>
    );
  }

  // Number → numeric input.
  if (typeof value === 'number') {
    return (
      <div className="space-y-1.5">
        <Label htmlFor={id} className="text-sm font-medium">
          {row.label}
        </Label>
        <Input
          id={id}
          type="number"
          value={Number.isFinite(value) ? String(value) : ''}
          disabled={disabled}
          onChange={(e) => {
            const raw = e.target.value;
            const parsed = raw === '' ? 0 : Number(raw);
            if (!Number.isFinite(parsed)) return;
            commitEdit(row.key, parsed, editableFields, onEdit);
          }}
        />
      </div>
    );
  }

  // String — long-form keys get a Textarea, everything else a single-line Input.
  const stringValue = typeof value === 'string' ? value : value == null ? '' : String(value);
  const isLong = LONG_TEXT_KEYS.has(row.key) || stringValue.length > TEXTAREA_PROMOTION_THRESHOLD;
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-sm font-medium">
        {row.label}
      </Label>
      {isLong ? (
        <Textarea
          id={id}
          rows={Math.min(
            TEXTAREA_MAX_ROWS,
            Math.max(TEXTAREA_MIN_ROWS, Math.ceil(stringValue.length / TEXTAREA_CHARS_PER_ROW))
          )}
          value={stringValue}
          disabled={disabled}
          onChange={(e) => commitEdit(row.key, e.target.value, editableFields, onEdit)}
        />
      ) : (
        <Input
          id={id}
          value={stringValue}
          disabled={disabled}
          onChange={(e) => commitEdit(row.key, e.target.value, editableFields, onEdit)}
        />
      )}
    </div>
  );
}

interface ReadOnlyRowProps {
  row: FieldRow;
  t: ReturnType<typeof useTranslations>;
}

function ReadOnlyRow({ row, t }: ReadOnlyRowProps) {
  return (
    <>
      <dt className="text-sm font-medium text-ink-mid">{row.label}</dt>
      <dd
        className={cn(
          'min-w-0 text-sm text-ink',
          // Long values can wrap; pre-line preserves newlines for content
          // like multi-line text but still wraps long lines responsively.
          'whitespace-pre-line break-words'
        )}
      >
        {formatReadOnly(row.value, t)}
      </dd>
    </>
  );
}

function formatReadOnly(value: unknown, t: ReturnType<typeof useTranslations>): string {
  if (value === null || value === undefined) return '—';
  if (typeof value === 'boolean') return value ? t('booleanYes') : t('booleanNo');
  if (typeof value === 'string') return value === '' ? '—' : value;
  if (typeof value === 'number') return String(value);
  // Object / array — pretty-print compactly so context stays readable.
  // Locked fields can carry structured values (e.g., metadata blobs) but
  // they are never editable, so a JSON one-liner is acceptable here.
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function resolveLabel(t: ReturnType<typeof useTranslations>, key: string): string {
  // next-intl exposes `has()` to probe key existence without throwing on
  // missing in strict mode. Tests stub it to always return true and look up
  // via the namespace; production behaviour matches.
  const hasKey = (t as unknown as { has?: (k: string) => boolean }).has;
  if (hasKey && hasKey(`fields.${key}`)) return t(`fields.${key}`);
  return t('fieldFallback', { key });
}

function commitEdit(
  key: string,
  value: string | number | boolean,
  editableFields: string[],
  onEdit: (k: string, v: string | number | boolean) => void
) {
  // The same gate used by the legacy JSON editor — keeps the safety
  // contract (root-only + scalar-only + whitelist) intact at the input
  // boundary even if future props widen to include nested forms.
  const accepted = evaluateEditGate(
    {
      value,
      oldValue: null,
      keyName: key,
      parentName: undefined,
      type: 'value',
    },
    editableFields
  );
  if (!accepted) return;
  onEdit(key, value);
}

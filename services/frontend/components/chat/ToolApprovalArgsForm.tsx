'use client';

import { Fragment, memo, useId, useState, type ReactNode } from 'react';
import { Lock } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { AppInput as Input } from '@/components/design-system/AppInput';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { AppTextarea as Textarea } from '@/components/design-system/AppInput';

import { evaluateEditGate } from './toolApprovalGate';

type Translator = ReturnType<typeof useTranslations>;

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

// Maximum recursion depth for read-only structured values (locked nested
// objects / arrays). Beyond this we fall back to a JSON one-liner so a
// pathologically deep payload can't blow the render stack or the viewport.
const READ_ONLY_MAX_DEPTH = 2;

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

  const editableRows = editableFields
    .filter((k) => k in args)
    .map((k) => rows.find((r) => r.key === k))
    .filter((r): r is FieldRow => r !== undefined);
  const lockedRows = rows.filter((r) => !editableSet.has(r.key));

  if (!editable) {
    return (
      <dl className="grid grid-cols-1 gap-y-2 text-sm sm:grid-cols-[96px_minmax(0,1fr)] sm:gap-x-3">
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
                persistedValue={args[row.key]}
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
          <dl className="grid grid-cols-1 gap-y-2 text-sm sm:grid-cols-[96px_minmax(0,1fr)] sm:gap-x-3">
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
  /** Original server value — used to lock the input type (integer-only vs decimal). */
  persistedValue: unknown;
  idPrefix: string;
  disabled: boolean;
  editableFields: string[];
  onEdit: (key: string, value: string | number | boolean) => void;
}

function EditableField({
  row,
  persistedValue,
  idPrefix,
  disabled,
  editableFields,
  onEdit,
}: EditableFieldProps) {
  const id = `${idPrefix}-${row.key}`;
  const value = row.value;

  if (typeof value === 'boolean') {
    return (
      <EditableBooleanField
        id={id}
        rowKey={row.key}
        label={row.label}
        value={value}
        disabled={disabled}
        editableFields={editableFields}
        onEdit={onEdit}
      />
    );
  }

  if (typeof value === 'number') {
    return (
      <EditableNumberField
        id={id}
        rowKey={row.key}
        label={row.label}
        value={value}
        persistedValue={persistedValue}
        disabled={disabled}
        editableFields={editableFields}
        onEdit={onEdit}
      />
    );
  }

  return (
    <EditableStringField
      id={id}
      rowKey={row.key}
      label={row.label}
      value={value}
      disabled={disabled}
      editableFields={editableFields}
      onEdit={onEdit}
    />
  );
}

interface BooleanFieldProps {
  id: string;
  rowKey: string;
  label: string;
  value: boolean;
  disabled: boolean;
  editableFields: string[];
  onEdit: (key: string, value: string | number | boolean) => void;
}

function EditableBooleanField({
  id,
  rowKey,
  label,
  value,
  disabled,
  editableFields,
  onEdit,
}: BooleanFieldProps) {
  return (
    <div className="flex items-center justify-between gap-3">
      <Label htmlFor={id} className="text-sm font-medium">
        {label}
      </Label>
      <Switch
        id={id}
        checked={value}
        disabled={disabled}
        onCheckedChange={(next) => commitEdit(rowKey, next, editableFields, onEdit)}
      />
    </div>
  );
}

interface NumberFieldProps {
  id: string;
  rowKey: string;
  label: string;
  value: number;
  persistedValue: unknown;
  disabled: boolean;
  editableFields: string[];
  onEdit: (key: string, value: string | number | boolean) => void;
}

function EditableNumberField({
  id,
  rowKey,
  label,
  value,
  persistedValue,
  disabled,
  editableFields,
  onEdit,
}: NumberFieldProps) {
  const integerOnly =
    typeof persistedValue === 'number' && Number.isFinite(persistedValue)
      ? Number.isInteger(persistedValue)
      : false;

  const [raw, setRaw] = useState<string>(() => (Number.isFinite(value) ? String(value) : ''));

  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-sm font-medium">
        {label}
      </Label>
      <Input
        id={id}
        type="number"
        inputMode={integerOnly ? 'numeric' : 'decimal'}
        step={integerOnly ? '1' : 'any'}
        value={raw}
        disabled={disabled}
        onChange={(e) => {
          const next = e.target.value;
          setRaw(next);
          if (next === '' || next === '-') return;
          const parsed = Number(next);
          if (!Number.isFinite(parsed)) return;
          if (integerOnly && !Number.isInteger(parsed)) return;
          commitEdit(rowKey, parsed, editableFields, onEdit);
        }}
      />
    </div>
  );
}

interface StringFieldProps {
  id: string;
  rowKey: string;
  label: string;
  value: unknown;
  disabled: boolean;
  editableFields: string[];
  onEdit: (key: string, value: string | number | boolean) => void;
}

function EditableStringField({
  id,
  rowKey,
  label,
  value,
  disabled,
  editableFields,
  onEdit,
}: StringFieldProps) {
  const stringValue = typeof value === 'string' ? value : value == null ? '' : String(value);
  const isLong = LONG_TEXT_KEYS.has(rowKey) || stringValue.length > TEXTAREA_PROMOTION_THRESHOLD;
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-sm font-medium">
        {label}
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
          onChange={(e) => commitEdit(rowKey, e.target.value, editableFields, onEdit)}
        />
      ) : (
        <Input
          id={id}
          value={stringValue}
          disabled={disabled}
          onChange={(e) => commitEdit(rowKey, e.target.value, editableFields, onEdit)}
        />
      )}
    </div>
  );
}

interface ReadOnlyRowProps {
  row: FieldRow;
  t: Translator;
}

function ReadOnlyRow({ row, t }: ReadOnlyRowProps) {
  return (
    <>
      <dt className="text-sm font-medium text-ink-mid">{row.label}</dt>
      <dd className="min-w-0 whitespace-pre-line break-words text-reading text-ink">
        <ReadOnlyValue value={row.value} t={t} />
      </dd>
    </>
  );
}

interface ReadOnlyValueProps {
  value: unknown;
  t: Translator;
  depth?: number;
}

function ReadOnlyValue({ value, t, depth = 0 }: ReadOnlyValueProps): ReactNode {
  if (value === null || value === undefined) return '—';
  if (typeof value === 'boolean') return value ? t('booleanYes') : t('booleanNo');
  if (typeof value === 'string') return value === '' ? '—' : value;
  if (typeof value === 'number') return String(value);

  if (depth >= READ_ONLY_MAX_DEPTH) {
    return <code className="text-xs text-ink-mid">{safeStringify(value)}</code>;
  }

  if (Array.isArray(value)) {
    if (value.length === 0) return '—';
    return (
      <ul className="list-disc space-y-0.5 pl-4">
        {value.map((item, idx) => (
          <li key={idx}>
            <ReadOnlyValue value={item} t={t} depth={depth + 1} />
          </li>
        ))}
      </ul>
    );
  }

  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) return '—';
    return (
      <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-xs">
        {entries.map(([k, v]) => (
          <Fragment key={k}>
            <dt className="text-ink-mid">{resolveLabel(t, k)}</dt>
            <dd className="min-w-0 whitespace-pre-line break-words text-ink">
              <ReadOnlyValue value={v} t={t} depth={depth + 1} />
            </dd>
          </Fragment>
        ))}
      </dl>
    );
  }

  return String(value);
}

function safeStringify(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function resolveLabel(t: Translator, key: string): string {
  if (t.has(`fields.${key}`)) return t(`fields.${key}`);
  return t('fieldFallback', { key });
}

function commitEdit(
  key: string,
  value: string | number | boolean,
  editableFields: string[],
  onEdit: (k: string, v: string | number | boolean) => void
) {
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

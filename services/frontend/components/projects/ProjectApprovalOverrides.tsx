'use client';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { cn } from '@/lib/utils';
import { PLATFORM_FULL_LABELS } from '@/lib/platforms';
import { groupByPlatform, type PlatformKey } from '@/lib/hooks/useTools';
import { toolLabel, type Tool, type ToolApprovals, type ToolApprovalValue } from '@/lib/schemas';

interface ProjectApprovalOverridesProps {
  tools: Tool[];
  // The business-level defaults — rendered next to the Inherit radio so the
  // user sees the effective policy for each inherit-ed tool.
  businessApprovals: ToolApprovals;
  // The project-level overrides. Keys present in this map override the
  // business default; ABSENT KEYS mean «как у бизнеса» (inherit).
  value: Record<string, ToolApprovalValue>;
  onChange: (next: Record<string, ToolApprovalValue>) => void;
}

// Phase 16 — POLICY-06 frontend. Sits beneath the Phase 15 WhitelistRadio on
// /projects/:id. 3-way per-tool radio group:
//
//   - «Автоматически»   → value[name] = "auto"
//   - «Вручную»         → value[name] = "manual"
//   - «как у бизнеса»   → key DELETED from value (inherit == absence)
//
// The literal label «как у бизнеса» is enforced by 16-CONTEXT D-21 and is
// asserted by the plan's grep acceptance criteria.

const SELECTION_INHERIT = 'inherit' as const;
const SELECTION_AUTO = 'auto' as const;
const SELECTION_MANUAL = 'manual' as const;

type Selection = typeof SELECTION_INHERIT | typeof SELECTION_AUTO | typeof SELECTION_MANUAL;

const PLATFORM_DISPLAY_ORDER: PlatformKey[] = [
  'telegram',
  'vk',
  'yandex_business',
  'google_business',
];

function selectionFor(
  toolName: string,
  value: Record<string, ToolApprovalValue>
): Selection {
  const explicit = value[toolName];
  if (explicit === 'auto') return SELECTION_AUTO;
  if (explicit === 'manual') return SELECTION_MANUAL;
  return SELECTION_INHERIT;
}

// applySelection computes the next value map for a given radio choice.
// Inherit is encoded as KEY ABSENCE — we return a NEW map without the key.
function applySelection(
  current: Record<string, ToolApprovalValue>,
  toolName: string,
  selection: Selection
): Record<string, ToolApprovalValue> {
  if (selection === SELECTION_INHERIT) {
    if (!(toolName in current)) return current;
    const next: Record<string, ToolApprovalValue> = {};
    for (const [k, v] of Object.entries(current)) {
      if (k !== toolName) next[k] = v;
    }
    return next;
  }
  return { ...current, [toolName]: selection };
}

function businessDefaultLabel(value: ToolApprovalValue | undefined): string {
  if (value === 'auto') return 'Автоматически';
  // Backend default for an unset key is effectively "manual" (POLICY-05
  // — no explicit entry ⇒ manual-floor tools require approval).
  return 'Вручную';
}

function ToolRow({
  tool,
  value,
  businessDefault,
  onChange,
}: {
  tool: Tool;
  value: Record<string, ToolApprovalValue>;
  businessDefault: ToolApprovalValue | undefined;
  onChange: (next: Record<string, ToolApprovalValue>) => void;
}) {
  const selection = selectionFor(tool.name, value);
  const autoId = `po-auto-${tool.name}`;
  const manualId = `po-manual-${tool.name}`;
  const inheritId = `po-inherit-${tool.name}`;

  return (
    <div className="space-y-2 rounded-md border p-3">
      <div>
        <p className="text-sm font-medium">{toolLabel(tool)}</p>
        {tool.description && (
          <p className="mt-1 text-xs text-muted-foreground">{tool.description}</p>
        )}
      </div>
      <RadioGroup
        value={selection}
        onValueChange={(next) =>
          onChange(applySelection(value, tool.name, next as Selection))
        }
        className="flex flex-wrap items-center gap-4"
      >
        <div className="flex items-center gap-2">
          <RadioGroupItem value={SELECTION_AUTO} id={autoId} />
          <Label htmlFor={autoId} className="text-sm cursor-pointer">
            Автоматически
          </Label>
        </div>
        <div className="flex items-center gap-2">
          <RadioGroupItem value={SELECTION_MANUAL} id={manualId} />
          <Label htmlFor={manualId} className="text-sm cursor-pointer">
            Вручную
          </Label>
        </div>
        <div className="flex items-center gap-2">
          <RadioGroupItem value={SELECTION_INHERIT} id={inheritId} />
          <Label htmlFor={inheritId} className="text-sm cursor-pointer">
            «как у бизнеса»
          </Label>
          <span
            className={cn(
              'rounded-md border px-2 py-0.5 text-xs text-muted-foreground',
              selection === SELECTION_INHERIT ? 'border-primary/40 bg-accent/40' : 'border-border'
            )}
            aria-label={`Бизнес: ${businessDefaultLabel(businessDefault)}`}
          >
            Бизнес: {businessDefaultLabel(businessDefault)}
          </span>
        </div>
      </RadioGroup>
    </div>
  );
}

export function ProjectApprovalOverrides({
  tools,
  businessApprovals,
  value,
  onChange,
}: ProjectApprovalOverridesProps) {
  // Only manual-floor tools get a 3-way toggle. Forbidden tools can never
  // be enabled (POLICY-01); auto-floor tools bypass HITL — neither is
  // meaningful here.
  const manualTools = tools.filter((t) => t.floor === 'manual');
  const buckets = groupByPlatform(manualTools);
  const platforms = PLATFORM_DISPLAY_ORDER.filter((p) => buckets[p].length > 0);

  if (manualTools.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Нет инструментов, требующих одобрения — всё работает автоматически.
      </p>
    );
  }

  return (
    <div className="space-y-4">
      {platforms.map((platform) => {
        const list = buckets[platform];
        if (list.length === 0) return null;
        const label = PLATFORM_FULL_LABELS[platform] ?? platform;
        return (
          <Card key={platform}>
            <CardHeader>
              <CardTitle className="text-sm">{label}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              {list.map((tool) => (
                <ToolRow
                  key={tool.name}
                  tool={tool}
                  value={value}
                  businessDefault={businessApprovals[tool.name]}
                  onChange={onChange}
                />
              ))}
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}

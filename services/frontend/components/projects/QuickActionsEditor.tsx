'use client';

import { Plus, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { MAX_QUICK_ACTIONS } from '@/lib/quick-actions';

interface QuickActionsEditorProps {
  value: string[];
  onChange: (items: string[]) => void;
}

export function QuickActionsEditor({ value, onChange }: QuickActionsEditorProps) {
  const updateItem = (index: number, text: string) => {
    const next = [...value];
    next[index] = text;
    onChange(next);
  };

  const removeItem = (index: number) => {
    const next = value.filter((_, i) => i !== index);
    onChange(next);
  };

  const addItem = () => {
    if (value.length >= MAX_QUICK_ACTIONS) return;
    onChange([...value, '']);
  };

  const atMax = value.length >= MAX_QUICK_ACTIONS;

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        До 6 коротких фраз — появятся кнопками в пустом чате.
      </p>
      {value.length > 0 && (
        <ul className="space-y-2">
          {value.map((item, index) => (
            <li key={index} className="flex items-center gap-2">
              <Input
                value={item}
                onChange={(e) => updateItem(index, e.target.value)}
                placeholder="Например: Проверить отзывы"
                aria-label={`Быстрое действие ${index + 1}`}
                className="flex-1"
              />
              <Button
                type="button"
                variant="outline"
                size="icon"
                className="text-destructive hover:bg-destructive/10 hover:text-destructive"
                onClick={() => removeItem(index)}
                aria-label={`Удалить быстрое действие ${index + 1}`}
              >
                <Trash2 size={16} />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={addItem}
        disabled={atMax}
        className="gap-2"
      >
        <Plus size={14} />
        Добавить
      </Button>
    </div>
  );
}

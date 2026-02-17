'use client'

import { useState } from 'react'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

interface Props {
  platform: string
  open: boolean
  onClose: () => void
  onConnect: (credentials: Record<string, string>) => Promise<void>
}

const configs: Record<string, { label: string; fields: { key: string; label: string; placeholder: string }[] }> = {
  telegram: {
    label: 'Подключить Telegram',
    fields: [{ key: 'bot_token', label: 'Bot Token', placeholder: 'Получите у @BotFather' }],
  },
  vk: {
    label: 'Подключить ВКонтакте',
    fields: [{ key: 'access_token', label: 'Access Token', placeholder: 'Токен доступа VK' }],
  },
  yandex_business: {
    label: 'Подключить Яндекс.Бизнес',
    fields: [{ key: 'cookies', label: 'Cookies JSON', placeholder: 'Скопируйте cookies из браузера' }],
  },
}

export function ConnectDialog({ platform, open, onClose, onConnect }: Props) {
  const config = configs[platform]
  const [values, setValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)

  if (!config) return null

  const handleSubmit = async () => {
    setLoading(true)
    try {
      await onConnect(values)
      onClose()
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{config.label}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-2">
          {config.fields.map((field) => (
            <div key={field.key} className="space-y-1">
              <Label>{field.label}</Label>
              <Input
                placeholder={field.placeholder}
                value={values[field.key] ?? ''}
                onChange={(e) => setValues({ ...values, [field.key]: e.target.value })}
              />
            </div>
          ))}
          <Button onClick={handleSubmit} disabled={loading} className="w-full">
            {loading ? 'Подключение...' : 'Подключить'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

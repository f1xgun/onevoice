'use client'

import { useState } from 'react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface Props {
  open: boolean
  onClose: () => void
}

export function TelegramConnectModal({ open, onClose }: Props) {
  const [step, setStep] = useState(1)
  const [channelId, setChannelId] = useState('')
  const [loading, setLoading] = useState(false)

  const handleConnect = async () => {
    if (!channelId.trim()) return
    setLoading(true)
    try {
      await api.post('/integrations/telegram/connect', {
        channel_id: channelId.trim(),
      })
      toast.success('Telegram канал подключён!')
      handleClose()
    } catch {
      toast.error('Ошибка подключения. Убедитесь, что бот добавлен как администратор в канал.')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => {
    setStep(1)
    setChannelId('')
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Подключение Telegram</DialogTitle>
        </DialogHeader>

        {step === 1 && (
          <div className="space-y-4">
            <p className="text-sm text-gray-600">
              Для подключения Telegram канала необходимо добавить бота OneVoice
              в ваш канал как администратора.
            </p>
            <ol className="text-sm space-y-2 list-decimal list-inside text-gray-600">
              <li>Откройте ваш Telegram канал</li>
              <li>Перейдите в настройки канала → Администраторы</li>
              <li>Добавьте бота <code className="bg-gray-100 px-1 rounded">@OneVoiceBot</code></li>
              <li>Дайте боту право публиковать сообщения</li>
            </ol>
            <Button className="w-full" onClick={() => setStep(2)}>
              Далее
            </Button>
          </div>
        )}

        {step === 2 && (
          <div className="space-y-4">
            <p className="text-sm text-gray-600">
              Введите ID или username вашего канала (например, <code className="bg-gray-100 px-1 rounded">@mychannel</code> или <code className="bg-gray-100 px-1 rounded">-1001234567890</code>):
            </p>
            <Input
              placeholder="@channel или -100..."
              value={channelId}
              onChange={(e) => setChannelId(e.target.value)}
            />
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setStep(1)} className="flex-1">
                Назад
              </Button>
              <Button
                onClick={handleConnect}
                disabled={!channelId.trim() || loading}
                className="flex-1"
              >
                {loading ? 'Проверка...' : 'Подключить'}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

import { useState } from 'react'
import { Button } from '../../components/ui/Button'
import { Card, CardBody, CardHeader } from '../../components/ui/Card'
import { getErrorMessage } from '../../lib/errorMessage'
import { isMockMode } from '../../lib/capabilities'
import { useTPMWindowSettings, useUpdateTPMWindow } from './useSettings'

const WINDOW_PRESETS = [
  { value: 60, label: '1 分钟' },
  { value: 300, label: '5 分钟' },
  { value: 900, label: '15 分钟' },
  { value: 3600, label: '1 小时' },
  { value: 21600, label: '6 小时' },
  { value: 86400, label: '24 小时' },
]

function formatWindow(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

/**
 * 全局限流窗口设置。窗口长度是 gateway 运行时配置，
 * 与租户的 tpm_limit（每个窗口允许的 token 数）配套使用。
 */
export function TPMWindowCard() {
  const enabled = !isMockMode
  const windowQuery = useTPMWindowSettings(enabled)
  const updateWindow = useUpdateTPMWindow()
  const [windowDraft, setWindowDraft] = useState<number | null>(null)

  if (!enabled) {
    return null
  }

  // 已保存值与 gateway 实际生效值可能短暂不一致（3 秒轮询窗口）。
  const savedWindow = windowQuery.data
    ? windowQuery.data.window_seconds ||
      windowQuery.data.effective_window_seconds ||
      60
    : 60
  const windowValue = windowDraft ?? savedWindow
  const effectiveWindow = windowQuery.data?.effective_window_seconds ?? 0
  const windowApplyPending =
    windowQuery.data?.configured === true &&
    effectiveWindow > 0 &&
    effectiveWindow !== windowQuery.data.window_seconds

  const saveWindow = () => {
    updateWindow.mutate(windowValue, {
      onSuccess: () => setWindowDraft(null),
    })
  }

  return (
    <Card>
      <CardHeader
        title="TPM 配额窗口"
        description="全局限流窗口：所有租户的 tpm_limit 都按这个窗口计数。保存后 gateway 3 秒内热切换。"
      />
      <CardBody>
        {windowQuery.isError ? (
          <div className="callout callout--warning">
            <span>
              窗口配置读取失败：{getErrorMessage(windowQuery.error)}
            </span>
          </div>
        ) : (
          <div className="table-toolbar">
            <div className="table-toolbar__filters">
              <select
                className="field__input table-filter"
                value={windowValue}
                onChange={(event) => {
                  setWindowDraft(Number(event.target.value))
                  updateWindow.reset()
                }}
                aria-label="选择 TPM 窗口长度"
                disabled={windowQuery.isPending || updateWindow.isPending}
              >
                {!WINDOW_PRESETS.some(
                  (preset) => preset.value === windowValue,
                ) ? (
                  <option value={windowValue}>
                    {formatWindow(windowValue)}
                  </option>
                ) : null}
                {WINDOW_PRESETS.map((preset) => (
                  <option key={preset.value} value={preset.value}>
                    {preset.label}
                  </option>
                ))}
              </select>
              <Button
                variant="primary"
                size="sm"
                loading={updateWindow.isPending}
                disabled={
                  windowQuery.isPending ||
                  windowQuery.isError ||
                  (windowQuery.data?.configured === true &&
                    windowValue === savedWindow)
                }
                onClick={saveWindow}
              >
                保存窗口
              </Button>
            </div>
            <div className="table-toolbar__meta" aria-live="polite">
              {windowQuery.isPending
                ? '正在读取窗口配置…'
                : effectiveWindow > 0
                  ? windowApplyPending
                    ? `已保存 ${formatWindow(
                        windowQuery.data?.window_seconds ?? savedWindow,
                      )}，gateway 当前 ${formatWindow(
                        effectiveWindow,
                      )}（应用中）`
                    : `gateway 当前生效：${formatWindow(effectiveWindow)}`
                  : 'gateway 尚未上报窗口'}
            </div>
          </div>
        )}
        {updateWindow.isError ? (
          <div className="callout callout--warning" role="alert">
            <span>保存失败：{getErrorMessage(updateWindow.error)}</span>
          </div>
        ) : null}
        {updateWindow.isSuccess ? (
          <p className="page-note" role="status">
            已保存。gateway 轮询到新配置后会把窗口切换为{' '}
            {formatWindow(updateWindow.data?.window_seconds ?? windowValue)}
            ；旧窗口的计数不会迁移。
          </p>
        ) : null}
      </CardBody>
    </Card>
  )
}

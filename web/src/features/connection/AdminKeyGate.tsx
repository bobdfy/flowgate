import { useState, type FormEvent } from 'react'
import { Eye, EyeOff, KeyRound, Network } from 'lucide-react'
import { Button } from '../../components/ui/Button'
import { env } from '../../lib/env'
import { getErrorMessage } from '../../lib/errorMessage'
import { useConnection } from './useConnection'

export function AdminKeyGate() {
  const { authenticate, clearKey, keyConfigured, rememberKey, isChecking } =
    useConnection()
  const [value, setValue] = useState('')
  const [remember, setRemember] = useState(rememberKey)
  const [revealed, setRevealed] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const key = value.trim()
    if (!key) {
      setFormError('请输入管理密钥。')
      return
    }

    setSubmitting(true)
    setFormError(null)
    try {
      await authenticate(key, remember)
    } catch (error) {
      setFormError(getErrorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  const handleClear = () => {
    setValue('')
    setRemember(false)
    setFormError(null)
    clearKey()
  }

  return (
    <div className="gate">
      <div className="gate__panel">
        <div className="gate__brand">
          <span className="gate__mark">
            <Network size={18} strokeWidth={1.9} aria-hidden="true" />
          </span>
          <div>
            <div className="gate__brand-name">FlowGate Console</div>
            <div className="gate__brand-subtitle">API 网关管理控制台</div>
          </div>
        </div>

        <h1 className="gate__title">连接管理面</h1>
        <p className="gate__description">
          管理服务已启用 <code>X-Admin-Key</code> 鉴权。请输入启动 admin 时通过{' '}
          <code>-admin-key</code> 配置的密钥。
        </p>

        {keyConfigured ? (
          <div className="callout callout--warning" style={{ marginTop: 16 }}>
            <KeyRound size={14} className="callout__icon" aria-hidden="true" />
            <span>
              当前会话已保存一个密钥，但校验失败。你可以重新输入，或先清除已保存的密钥。
            </span>
          </div>
        ) : null}

        <form className="gate__form" onSubmit={handleSubmit}>
          <div className="field">
            <label className="field__label" htmlFor="admin-key">
              管理密钥
            </label>
            <div className="field__control">
              <input
                id="admin-key"
                className="field__input field__input--mono field__input--with-action"
                type={revealed ? 'text' : 'password'}
                value={value}
                onChange={(event) => setValue(event.target.value)}
                autoFocus
                autoComplete="off"
                spellCheck={false}
                placeholder="输入 X-Admin-Key"
                disabled={submitting}
                aria-invalid={formError ? true : undefined}
                aria-describedby={
                  formError ? 'admin-key-hint admin-key-error' : 'admin-key-hint'
                }
              />
              <button
                type="button"
                className="field__reveal"
                onClick={() => setRevealed((current) => !current)}
                aria-label={revealed ? '隐藏密钥' : '显示密钥'}
                disabled={submitting}
              >
                {revealed ? (
                  <EyeOff size={15} aria-hidden="true" />
                ) : (
                  <Eye size={15} aria-hidden="true" />
                )}
              </button>
            </div>
            <div className="field__hint" id="admin-key-hint">
              密钥通过 <code>X-Admin-Key</code> 请求头发送，不写入 URL 或日志。
            </div>
          </div>

          <label className="checkbox-row">
            <input
              type="checkbox"
              checked={remember}
              onChange={(event) => setRemember(event.target.checked)}
              disabled={submitting}
            />
            <span>在当前标签页记住密钥（sessionStorage，不写入 localStorage）</span>
          </label>

          {formError ? (
            <div className="field__error" id="admin-key-error" role="alert">
              {formError}
            </div>
          ) : null}

          <Button
            className="button--block"
            type="submit"
            variant="primary"
            size="lg"
            loading={submitting}
            disabled={!value.trim()}
          >
            验证并进入
          </Button>

          {keyConfigured ? (
            <Button
              className="button--block"
              variant="ghost"
              onClick={handleClear}
              disabled={submitting || isChecking}
            >
              清除已保存密钥
            </Button>
          ) : null}
        </form>

        <div className="gate__footer">
          当前管理面地址：<code>{env.adminBaseUrl}</code>
          <br />
          开发环境通过 Vite 代理转发到 <code>127.0.0.1:8092</code>。
        </div>
      </div>
    </div>
  )
}

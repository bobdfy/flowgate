import type { BadgeTone } from '../../components/ui/Badge'

export interface StatusMeta {
  label: string
  tone: BadgeTone
  description: string
}

export function enabledStatusMeta(enabled: boolean): StatusMeta {
  return enabled
    ? { label: '启用', tone: 'success', description: '参与发布快照' }
    : { label: '停用', tone: 'neutral', description: '不会进入发布快照' }
}

/**
 * health_status 是 upstream_nodes 表的持久化配置字段。
 * gateway 的实时健康检查结果只存在内存中，不会回写这个字段。
 */
export function nodeHealthStatusMeta(status: string): StatusMeta {
  switch (status) {
    case 'healthy':
      return {
        label: 'healthy',
        tone: 'success',
        description: '持久化字段值，不代表当前实时健康',
      }
    case 'unhealthy':
      return {
        label: 'unhealthy',
        tone: 'danger',
        description: '持久化字段值，不代表当前实时健康',
      }
    case 'unknown':
      return {
        label: 'unknown',
        tone: 'neutral',
        description: '持久化字段值，不代表当前实时健康',
      }
    default:
      return {
        label: status || '—',
        tone: 'neutral',
        description: '持久化字段值，不代表当前实时健康',
      }
  }
}

/** gateway 实时状态：来自健康检查 + 熔断器，与持久化 health_status 无关。 */
export function runtimeNodeStateMeta(state: string): StatusMeta {
  switch (state) {
    case 'closed':
      return {
        label: '健康',
        tone: 'success',
        description: '健康检查通过，正常参与负载均衡',
      }
    case 'half_open':
      return {
        label: '试探中',
        tone: 'warning',
        description: '熔断冷却结束，正在放行少量试探请求',
      }
    case 'open':
      return {
        label: '熔断中',
        tone: 'danger',
        description: '连续失败触发熔断，冷却到期前不再派发请求',
      }
    default:
      return {
        label: state || '—',
        tone: 'neutral',
        description: 'gateway 上报的原始状态值',
      }
  }
}

const protocolLabels: Record<string, string> = {
  http: 'HTTP',
  grpc: 'gRPC',
}

export function protocolLabel(protocol: string): string {
  return protocolLabels[protocol] ?? protocol ?? '—'
}

const overloadStrategyLabels: Record<string, string> = {
  'fail-fast': '快速失败',
  wait: '排队等待',
  drop: '排队丢弃',
}

export function overloadStrategyLabel(strategy: string): string {
  return overloadStrategyLabels[strategy] ?? strategy ?? '—'
}

const matchTypeLabels: Record<string, string> = {
  prefix: '前缀匹配',
  exact: '精确匹配',
}

export function matchTypeLabel(matchType: string): string {
  return matchTypeLabels[matchType] ?? matchType ?? '—'
}

export function methodsLabel(methods: string): string {
  const trimmed = methods.trim()
  return trimmed ? trimmed : '全部方法'
}

export function versionStatusMeta(status: string): StatusMeta {
  if (status === 'published') {
    return {
      label: '已发布',
      tone: 'success',
      description: '该版本的快照可被 gateway 加载',
    }
  }
  return {
    label: status || '—',
    tone: 'neutral',
    description: '后端返回的原始状态值',
  }
}

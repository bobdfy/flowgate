import type { AIModel, AIProvider } from '../../api/contracts/aiConfig'
import type { BadgeTone } from '../../components/ui/Badge'

export interface AIStatusMeta {
  label: string
  tone: BadgeTone
  description: string
}

export function aiProviderStatusMeta(provider: AIProvider): AIStatusMeta {
  if (!provider.enabled) {
    return {
      label: '已停用',
      tone: 'neutral',
      description: '提供商停用后不会进入 gateway 的 AI 运行时。',
    }
  }
  return {
    label: '已启用',
    tone: 'success',
    description:
      '配置已启用；能否加载还取决于 gateway 侧能否解析到密钥，控制台没有加载回执。',
  }
}

export interface KeySourceMeta {
  label: string
  tone: BadgeTone
  description: string
}

/**
 * gateway 解析密钥的顺序：先读 api_key_env 指向的环境变量，再使用数据库里的
 * 字面量 api_key。后者不会通过管理 API 返回，因此控制台无法确认它是否存在。
 */
export function providerKeySourceMeta(provider: AIProvider): KeySourceMeta {
  if (provider.api_key_env) {
    return {
      label: provider.api_key_env,
      tone: 'info',
      description:
        '环境变量名；变量值由 gateway 部署环境提供。控制台无法确认变量是否已设置。',
    }
  }
  return {
    label: '未指定环境变量',
    tone: 'warning',
    description:
      '管理 API 不返回库内字面量密钥。若数据库里也没有密钥，gateway 会跳过该提供商。',
  }
}

/**
 * 根据模型与提供商的配置推导运行时判断。控制台只能读配置，
 * 不能读 gateway 的加载结果，因此所有结论都限定在“是否会被尝试加载”。
 */
export function modelRuntimeMeta(
  model: AIModel,
  provider: AIProvider | undefined,
  providersReady: boolean,
): AIStatusMeta {
  if (!model.enabled) {
    return {
      label: '已停用',
      tone: 'neutral',
      description: '模型已停用，gateway 不会加载该模型别名。',
    }
  }
  if (!providersReady) {
    return {
      label: '待确认',
      tone: 'neutral',
      description: '提供商列表尚未成功加载，无法判断该模型的加载条件。',
    }
  }
  if (!provider) {
    return {
      label: '提供商缺失',
      tone: 'danger',
      description:
        '模型引用的提供商不在当前列表中，gateway 会跳过该模型。',
    }
  }
  if (!provider.enabled) {
    return {
      label: '不会加载',
      tone: 'warning',
      description: `提供商 ${provider.name} 已停用，gateway 会跳过该模型。`,
    }
  }
  if (!provider.api_key_env) {
    return {
      label: '待确认密钥',
      tone: 'warning',
      description: `提供商 ${provider.name} 未指定环境变量名；只有数据库里存在字面量密钥时才可能加载。`,
    }
  }
  return {
    label: '已启用',
    tone: 'success',
    description: `加载取决于环境变量 ${provider.api_key_env} 是否已设置；控制台没有加载回执。`,
  }
}

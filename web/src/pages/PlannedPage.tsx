import type { ReactNode } from 'react'
import { Info } from 'lucide-react'
import { Badge } from '../components/ui/Badge'
import { Card, CardBody, CardHeader } from '../components/ui/Card'
import { PageHeader } from '../components/layout/PageHeader'

export interface PlannedApi {
  method: string
  path: string
  note: string
}

export interface PlannedPageProps {
  eyebrow: string
  title: string
  description: string
  stage: string
  features: string[]
  apis?: PlannedApi[]
  noApiNote?: string
  note?: ReactNode
}

export function PlannedPage({
  eyebrow,
  title,
  description,
  stage,
  features,
  apis = [],
  noApiNote,
  note,
}: PlannedPageProps) {
  return (
    <div className="page">
      <PageHeader
        eyebrow={eyebrow}
        title={title}
        description={description}
        actions={
          <Badge tone="warning" dot>
            {stage}
          </Badge>
        }
      />

      <div className="callout callout--warning">
        <Info size={14} className="callout__icon" aria-hidden="true" />
        <span>
          本页面尚未接入运行时数据。当前只展示实现范围与真实 API 依赖，不使用
          Mock 或静态数据冒充真实状态。
        </span>
      </div>

      <div className="placeholder-grid">
        <Card>
          <CardHeader
            title="计划能力"
            description="实现时按以下范围逐步接入，不一次性铺开"
          />
          <CardBody>
            <ul className="plain-list">
              {features.map((feature) => (
                <li key={feature}>{feature}</li>
              ))}
            </ul>
          </CardBody>
        </Card>

        <Card>
          <CardHeader
            title="依赖的管理 API"
            description="路由来自 internal/api/handler.go"
          />
          <CardBody>
            {apis.length > 0 ? (
              <ul className="api-list api-list--detailed">
                {apis.map((api) => (
                  <li key={`${api.method} ${api.path}`}>
                    <span className="api-list__method">{api.method}</span>
                    <code className="api-list__path">{api.path}</code>
                    <span className="api-list__note">{api.note}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <div className="callout callout--warning">
                <span>
                  {noApiNote ?? '后端尚未提供本页面所需的数据接口。'}
                </span>
              </div>
            )}
          </CardBody>
        </Card>
      </div>

      {note ? (
        <Card>
          <CardHeader title="实现约束" description="接入前必须确认的后端语义" />
          <CardBody>
            <div className="text-sm visually-muted">{note}</div>
          </CardBody>
        </Card>
      ) : null}
    </div>
  )
}

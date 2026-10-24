import { PlannedPage } from './PlannedPage'

export function LogsPage() {
  return (
    <PlannedPage
      eyebrow="运行"
      title="请求日志"
      description="按时间、状态码、路由与 Request ID 检索请求并定位错误。"
      stage="待后端接口"
      features={[
        '按时间范围、状态码、路由与 Request ID 筛选',
        '展示请求耗时、上游结果与错误摘要',
        '查看单条请求详情与关联 Request ID',
        '分页、空结果与查询失败状态',
      ]}
      noApiNote="后端尚未提供日志持久化与查询 API。当前 gateway access log 仅输出到 stdout，控制台无法查询历史请求。"
      note={
        <>
          <p>
            在日志存储与查询 API 实现前，本页保持待接入状态，不展示样例日志或模拟筛选结果。
          </p>
          <p style={{ marginTop: 8 }}>
            后续需要明确保留周期、字段白名单、分页上限与敏感字段脱敏规则。
          </p>
        </>
      }
    />
  )
}

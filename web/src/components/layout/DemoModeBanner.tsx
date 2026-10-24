import { FlaskConical } from 'lucide-react'

/**
 * Mock 模式的全局标识。演示数据必须与真实运行数据明确区分，
 * 因此这条横幅在应用壳内始终可见，且不提供关闭入口。
 */
export function DemoModeBanner() {
  return (
    <div className="demo-banner" role="status">
      <FlaskConical size={15} aria-hidden="true" />
      <span>
        <strong>演示模式</strong>
        ：当前页面数据来自浏览器内 Mock
        适配层，不会访问真实管理面、数据库或 gateway。刷新页面会重置演示数据。
      </span>
    </div>
  )
}

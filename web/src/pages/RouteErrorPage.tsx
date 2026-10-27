import { Link, isRouteErrorResponse, useRouteError } from 'react-router-dom'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'

export function RouteErrorPage() {
  const error = useRouteError()
  const message = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : error instanceof Error
      ? error.message
      : '发生未知渲染错误。'

  return (
    <div className="error-page">
      <div className="error-page__panel">
        <Badge tone="danger" dot>
          页面错误
        </Badge>
        <h1 className="error-page__title">控制台渲染失败</h1>
        <p className="error-page__description">
          {message}
          <br />
          刷新页面后重试；如果问题持续存在，请检查浏览器控制台与前端构建输出。
        </p>
        <div className="error-page__actions">
          <Button variant="primary" onClick={() => window.location.reload()}>
            刷新页面
          </Button>
          <Link to="/overview" className="button button--secondary">
            返回运行总览
          </Link>
        </div>
      </div>
    </div>
  )
}

import { Link } from 'react-router-dom'

export function NotFoundPage() {
  return (
    <div className="page">
      <div className="not-found">
        <div className="not-found__code">404</div>
        <h1 className="not-found__title">页面不存在</h1>
        <p className="not-found__description">
          当前地址没有对应的控制台页面。请从侧边栏选择功能，或返回运行总览。
        </p>
        <div className="not-found__actions">
          <Link to="/overview" className="button button--primary">
            返回运行总览
          </Link>
        </div>
      </div>
    </div>
  )
}

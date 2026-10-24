/**
 * 路由级代码分割的加载占位。只有首次进入某个页面、对应 chunk 尚未下载时才会出现，
 * 页面内部的异步数据仍由各页面自己的 Skeleton / 错误状态处理。
 */
export function RouteLoading() {
  return (
    <div className="page" aria-busy="true">
      <div className="route-loading" role="status">
        <span className="spinner spinner--lg" aria-hidden="true" />
        <span>正在加载页面…</span>
      </div>
    </div>
  )
}

// Package adminui 提供管理面 HTTP 的通用件：静态鉴权中间件与单页应用托管。
package adminui

import (
	"crypto/subtle"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// RequireAdminKey 给管理面加静态鉴权：X-Admin-Key 必须与启动参数一致（常数时间比较）。
//
// publicStatic=true（同时托管前端）时只保护 /api/ 开头的请求：
// 页面本身必须能匿名加载，浏览器里再输入管理密钥。
func RequireAdminKey(key string, next http.Handler, publicStatic bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicStatic && !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Admin-Key")), []byte(key)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SPAHandler 托管单页应用：命中静态文件就返回文件，
// 其余路径（/tenants、/ai/models 等前端路由）回退 index.html。
func SPAHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// 未注册的 /api/... 保持 404，不要被 SPA 回退成 index.html。
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		target := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			// Vite 产物文件名带内容哈希，可以长缓存；HTML 必须每次回源校验，
			// 否则重新 pnpm build 后浏览器还在跑旧 bundle。
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}

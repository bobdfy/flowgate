package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// 本文件提供 AI provider / model 的管理 API。
//
// 设计要点：
//   - DB 是唯一事实来源，写成功后 gateway 通过 ai_config_version 感知变更；
//   - 密钥只存环境变量名（api_key_env），API 不接受明文 key；
//   - provider.name 与 model.name 会被客户端直接使用，因此限制字符集。

var (
	aiProviderNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	aiModelNameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	envNameRe        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// aiProviderRequest 是 provider 的写请求。
// Enabled 用指针，区分「没传」(nil → 默认 true) 和显式 false。
type aiProviderRequest struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Endpoint     string `json:"endpoint"`
	UpstreamPath string `json:"upstream_path"`
	APIKeyEnv    string `json:"api_key_env"`
	StreamUsage  bool   `json:"stream_usage"`
	Enabled      *bool  `json:"enabled"`
}

type aiModelRequest struct {
	Name          string `json:"name"`
	ProviderName  string `json:"provider_name"`
	UpstreamModel string `json:"upstream_model"`

	// 指针类型：不传表示不改（新建时取 0 = 不限），传 0 才能明确设成不限。
	MaxConcurrency *int  `json:"max_concurrency"`
	Enabled        *bool `json:"enabled"`
}

// ---- provider ----

// ListAIProviders 处理 GET /api/v1/ai/providers
func (h *Handler) ListAIProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.aiconfig.ListProviders(r.Context())
	if err != nil {
		writeAIStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

// CreateAIProvider 处理 POST /api/v1/ai/providers
func (h *Handler) CreateAIProvider(w http.ResponseWriter, r *http.Request) {
	var req aiProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p := model.AIProvider{}
	if err := applyAIProviderRequest(&p, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.aiconfig.CreateProvider(r.Context(), &p); err != nil {
		writeAIStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// UpdateAIProvider 处理 PUT /api/v1/ai/providers/{id}
func (h *Handler) UpdateAIProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	// 先取旧记录：api_key 不在请求体里，必须保留原值
	// （本地假上游用字面量 key，清掉就调不通了）。
	existing, err := h.aiconfig.GetProviderByID(r.Context(), id)
	if err != nil {
		writeAIStoreError(w, err)
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	var req aiProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p := *existing
	if err := applyAIProviderRequest(&p, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.aiconfig.UpdateProvider(r.Context(), &p); err != nil {
		writeAIStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// DeleteAIProvider 处理 DELETE /api/v1/ai/providers/{id}
func (h *Handler) DeleteAIProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.aiconfig.DeleteProvider(r.Context(), id); err != nil {
		writeAIStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- model ----

// ListAIModels 处理 GET /api/v1/ai/models
func (h *Handler) ListAIModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.aiconfig.ListModels(r.Context())
	if err != nil {
		writeAIStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models)
}

// CreateAIModel 处理 POST /api/v1/ai/models
func (h *Handler) CreateAIModel(w http.ResponseWriter, r *http.Request) {
	var req aiModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	m := model.AIModel{}
	if err := applyAIModelRequest(&m, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.ensureAIProvider(r.Context(), m.ProviderName); err != nil {
		writeAIStoreError(w, err)
		return
	}
	if err := h.aiconfig.CreateModel(r.Context(), &m); err != nil {
		writeAIStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// UpdateAIModel 处理 PUT /api/v1/ai/models/{name}
// 模型别名是主键，不能通过此接口改名（要改就删了重建）。
func (h *Handler) UpdateAIModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !aiModelNameRe.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid model name")
		return
	}

	var req aiModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = name

	m := model.AIModel{}
	if err := applyAIModelRequest(&m, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.ensureAIProvider(r.Context(), m.ProviderName); err != nil {
		writeAIStoreError(w, err)
		return
	}
	if err := h.aiconfig.UpdateModel(r.Context(), &m); err != nil {
		writeAIStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// DeleteAIModel 处理 DELETE /api/v1/ai/models/{name}
func (h *Handler) DeleteAIModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !aiModelNameRe.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid model name")
		return
	}
	if err := h.aiconfig.DeleteModel(r.Context(), name); err != nil {
		writeAIStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- 校验与错误映射 ----

// applyAIProviderRequest 把请求写入 provider 并做校验（Create / Update 共用）。
func applyAIProviderRequest(p *model.AIProvider, req aiProviderRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errors.New("name is required")
	}
	if !aiProviderNameRe.MatchString(name) {
		return errors.New("name may only contain letters, digits, dot, dash and underscore")
	}

	if req.Type == "" {
		req.Type = "openai"
	}
	if req.Type != "openai" {
		return errors.New("type must be openai (only OpenAI-compatible upstreams are supported)")
	}

	endpoint := strings.TrimRight(strings.TrimSpace(req.Endpoint), "/")
	if endpoint == "" {
		return errors.New("endpoint is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("endpoint must be a full http/https URL")
	}

	upstreamPath := strings.TrimSpace(req.UpstreamPath)
	if upstreamPath != "" && !strings.HasPrefix(upstreamPath, "/") {
		return errors.New("upstream_path must start with /")
	}

	apiKeyEnv := strings.TrimSpace(req.APIKeyEnv)
	if apiKeyEnv != "" && !envNameRe.MatchString(apiKeyEnv) {
		return errors.New("api_key_env must be a valid environment variable name")
	}

	p.Name = name
	p.Type = req.Type
	p.Endpoint = endpoint
	p.UpstreamPath = upstreamPath
	p.APIKeyEnv = apiKeyEnv
	p.StreamUsage = req.StreamUsage
	p.Enabled = req.Enabled == nil || *req.Enabled
	return nil
}

// applyAIModelRequest 把请求写入 model 并做校验（Create / Update 共用）。
func applyAIModelRequest(m *model.AIModel, req aiModelRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errors.New("name is required")
	}
	if !aiModelNameRe.MatchString(name) {
		return errors.New("name may only contain letters, digits, dot, colon, dash and underscore")
	}
	provider := strings.TrimSpace(req.ProviderName)
	if provider == "" {
		return errors.New("provider_name is required")
	}

	if req.MaxConcurrency != nil {
		if *req.MaxConcurrency < 0 {
			return errors.New("max_concurrency must be >= 0 (0 means unlimited)")
		}
		m.MaxConcurrency = *req.MaxConcurrency
	}

	m.Name = name
	m.ProviderName = provider
	m.UpstreamModel = strings.TrimSpace(req.UpstreamModel)
	m.Enabled = req.Enabled == nil || *req.Enabled
	return nil
}

// ensureAIProvider 在写 model 前确认 provider 存在，返回更友好的 400。
func (h *Handler) ensureAIProvider(rctx context.Context, name string) error {
	p, err := h.aiconfig.GetProviderByName(rctx, name)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("%w: provider %q does not exist", errAIInvalid, name)
	}
	return nil
}

// errAIInvalid 表示请求引用了不存在 / 不合法的关联对象。
var errAIInvalid = errors.New("invalid ai config")

// writeAIStoreError 把 store 错误映射成合适的 HTTP 状态码。
func writeAIStoreError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, postgres.ErrAINotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, errAIInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		writeError(w, http.StatusConflict, "name already exists")
	case errors.As(err, &pgErr) && pgErr.Code == "23503":
		writeError(w, http.StatusBadRequest, "provider does not exist")
	default:
		log.Println("ai config error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

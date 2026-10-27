export type ApiErrorKind =
  | 'network'
  | 'timeout'
  | 'canceled'
  | 'unauthorized'
  | 'forbidden'
  | 'not-found'
  | 'conflict'
  | 'validation'
  | 'server'
  | 'client'
  | 'unknown'

export class ApiError extends Error {
  readonly kind: ApiErrorKind
  readonly status: number | null

  constructor(
    message: string,
    kind: ApiErrorKind = 'unknown',
    status: number | null = null,
  ) {
    super(message)
    this.name = 'ApiError'
    this.kind = kind
    this.status = status
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

export function isUnauthorizedError(error: unknown): boolean {
  return isApiError(error) && error.status === 401
}

export function toApiError(error: unknown): ApiError {
  if (isApiError(error)) {
    return error
  }
  if (error instanceof Error) {
    return new ApiError(error.message, 'unknown')
  }
  return new ApiError('未知错误', 'unknown')
}

export type ErrorCode =
  | 'AUTH_REQUIRED'
  | 'BAD_REQUEST'
  | 'PAYLOAD_TOO_LARGE'
  | 'UNSUPPORTED_MEDIA_TYPE'
  | 'RATE_LIMITED'
  | 'VALIDATION_ERROR'
  | 'NOT_FOUND'
  | 'CONFLICT'
  | 'UPSTREAM_TIMEOUT'
  | 'UPSTREAM_REJECTED'
  | 'UPSTREAM_RESPONSE_TOO_LARGE'
  | 'UPSTREAM_REDIRECT_BLOCKED'
  | 'MANUAL_ACTION_REQUIRED'
  | 'SITE_URL_BLOCKED'
  | 'INTERNAL_ERROR';

export class AppError extends Error {
  readonly statusCode: number;
  readonly code: ErrorCode;
  readonly retryable: boolean;

  constructor(statusCode: number, code: ErrorCode, message: string, retryable = false, options?: ErrorOptions) {
    super(message, options);
    this.name = 'AppError';
    this.statusCode = statusCode;
    this.code = code;
    this.retryable = retryable;
  }
}

export function asAppError(error: unknown): AppError {
  if (error instanceof AppError) return error;
  const statusCode = typeof error === 'object' && error !== null && 'statusCode' in error
    ? (error as { statusCode?: unknown }).statusCode
    : undefined;
  if (statusCode === 400) return new AppError(400, 'BAD_REQUEST', 'Malformed request');
  if (statusCode === 413) return new AppError(413, 'PAYLOAD_TOO_LARGE', 'Request body is too large');
  if (statusCode === 415) return new AppError(415, 'UNSUPPORTED_MEDIA_TYPE', 'Unsupported request media type');
  if (statusCode === 429) return new AppError(429, 'RATE_LIMITED', 'Too many requests', true);
  return new AppError(500, 'INTERNAL_ERROR', 'Internal server error', false, { cause: error });
}

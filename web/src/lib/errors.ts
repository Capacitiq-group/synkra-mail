export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fix?: string,
  ) { super(message); this.name = 'ApiError'; }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError;
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  if (typeof e === 'string') return e;
  return 'Something went wrong';
}

/** Surfaces { code, message, fix } shape — register §1.10. */
export function errorSurface(e: unknown): { code: string; message: string; fix?: string } {
  if (e instanceof ApiError) return { code: e.code, message: e.message, fix: e.fix };
  if (e instanceof Error) return { code: 'unknown', message: e.message };
  return { code: 'unknown', message: 'Something went wrong' };
}

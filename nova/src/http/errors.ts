export class HttpError extends Error {
  public readonly status: number;
  public readonly code: string;

  public constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export function notFound(method: string, path: string): HttpError {
  return new HttpError(404, "NOT_FOUND", `Route not found: ${method} ${path}`);
}

export function invalidJson(): HttpError {
  return new HttpError(400, "INVALID_JSON", "Request body is not valid JSON");
}

export function invalidUtf8(): HttpError {
  return new HttpError(400, "INVALID_UTF8", "Request body is not valid UTF-8");
}

export function invalidRequestUrl(): HttpError {
  return new HttpError(400, "INVALID_REQUEST_URL", "Request URL is not valid");
}

export function payloadTooLarge(limitBytes: number): HttpError {
  return new HttpError(413, "PAYLOAD_TOO_LARGE", `Request body exceeds ${limitBytes} bytes`);
}

export interface ErrorEnvelope {
  ok: false;
  error: {
    code: string;
    message: string;
  };
}

export function toErrorResponse(error: unknown): {
  status: number;
  body: ErrorEnvelope;
} {
  if (error instanceof HttpError) {
    return {
      status: error.status,
      body: {
        ok: false,
        error: {
          code: error.code,
          message: error.message
        }
      }
    };
  }

  return {
    status: 500,
    body: {
      ok: false,
      error: {
        code: "INTERNAL_ERROR",
        message: "Internal server error"
      }
    }
  };
}

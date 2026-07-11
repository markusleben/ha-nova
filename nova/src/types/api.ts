export interface ApiSuccess<T> {
  ok: true;
  data: T;
}

export interface ApiError {
  ok: false;
  error: {
    code: string;
    message: string;
  };
}

export type ApiResponse<T> = ApiSuccess<T> | ApiError;

export type CoreProxyMethod = "GET" | "POST" | "DELETE";

export interface CoreProxyRequest {
  method: CoreProxyMethod;
  path: string;
  body?: unknown;
}

export interface CoreProxyResponse {
  status: number;
  body: unknown;
  /**
   * Set to "base64" when the upstream body is binary (e.g. a camera JPEG).
   * Absent for JSON and text bodies, so every existing consumer is unaffected.
   */
  body_encoding?: "base64";
  content_type?: string;
}

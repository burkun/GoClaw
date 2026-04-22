/**
 * Error classification utilities for handling API errors.
 * Detects rate limits, quota issues, auth failures, and other common errors.
 */

export type ErrorType =
  | "rate_limit"
  | "quota_exceeded"
  | "auth_failed"
  | "provider_busy"
  | "network"
  | "unknown";

export interface ClassifiedError {
  type: ErrorType;
  title: string;
  message: string;
  retryable: boolean;
  retryAfter?: number; // seconds
}

// Patterns for detecting error types
const RATE_LIMIT_PATTERNS = [
  "rate limit",
  "too many requests",
  "429",
  "慢点",
  "请求过快",
  "频率限制",
];

const QUOTA_PATTERNS = [
  "quota",
  "insufficient_quota",
  "billing",
  "credit",
  "payment",
  "余额不足",
  "超出限额",
  "额度不足",
  "欠费",
];

const AUTH_PATTERNS = [
  "authentication",
  "unauthorized",
  "invalid api key",
  "invalid_api_key",
  "permission denied",
  "forbidden",
  "access denied",
  "无权",
  "未授权",
];

const BUSY_PATTERNS = [
  "server busy",
  "temporarily unavailable",
  "try again later",
  "please retry",
  "overloaded",
  "high demand",
  "服务繁忙",
  "稍后重试",
  "负载较高",
];

/**
 * Classifies an error and returns user-friendly information.
 */
export function classifyError(error: unknown): ClassifiedError {
  const errorStr = extractErrorString(error).toLowerCase();
  const retryAfter = extractRetryAfter(error);

  // Check for rate limit
  if (matchesPatterns(errorStr, RATE_LIMIT_PATTERNS)) {
    return {
      type: "rate_limit",
      title: "Rate Limit Reached",
      message: retryAfter
        ? `Too many requests. Please wait ${retryAfter} seconds before retrying.`
        : "Too many requests. Please wait a moment before continuing.",
      retryable: true,
      retryAfter,
    };
  }

  // Check for quota exceeded
  if (matchesPatterns(errorStr, QUOTA_PATTERNS)) {
    return {
      type: "quota_exceeded",
      title: "Quota Exceeded",
      message:
        "The AI provider quota has been exceeded. Please check your billing or try again later.",
      retryable: false,
    };
  }

  // Check for auth failure
  if (matchesPatterns(errorStr, AUTH_PATTERNS)) {
    return {
      type: "auth_failed",
      title: "Authentication Failed",
      message:
        "The AI provider credentials are invalid. Please check your API key configuration.",
      retryable: false,
    };
  }

  // Check for provider busy
  if (matchesPatterns(errorStr, BUSY_PATTERNS)) {
    return {
      type: "provider_busy",
      title: "Provider Busy",
      message:
        "The AI provider is temporarily unavailable. Please wait and try again.",
      retryable: true,
      retryAfter: retryAfter ?? 5,
    };
  }

  // Check for network errors
  if (
    errorStr.includes("network") ||
    errorStr.includes("connection") ||
    errorStr.includes("timeout") ||
    errorStr.includes("fetch") ||
    errorStr.includes("econnrefused") ||
    errorStr.includes("enotfound")
  ) {
    return {
      type: "network",
      title: "Network Error",
      message: "Unable to connect to the server. Please check your network connection.",
      retryable: true,
      retryAfter: 3,
    };
  }

  // Default to unknown error
  return {
    type: "unknown",
    title: "Error",
    message: extractErrorString(error) || "An unexpected error occurred. Please try again.",
    retryable: true,
  };
}

/**
 * Extracts a human-readable error string from various error types.
 */
function extractErrorString(error: unknown): string {
  if (typeof error === "string") {
    return error;
  }

  if (error instanceof Error) {
    return error.message || error.name;
  }

  if (typeof error === "object" && error !== null) {
    // Check for common error shapes
    const obj = error as Record<string, unknown>;

    if (typeof obj.message === "string") {
      return obj.message;
    }

    if (typeof obj.error === "string") {
      return obj.error;
    }

    if (obj.error instanceof Error) {
      return obj.error.message;
    }

    // Try JSON stringify as fallback
    try {
      return JSON.stringify(error);
    } catch {
      // Ignore
    }
  }

  return "Unknown error";
}

/**
 * Extracts retry-after value from error (in seconds).
 */
function extractRetryAfter(error: unknown): number | undefined {
  if (typeof error !== "object" || error === null) {
    return undefined;
  }

  const obj = error as Record<string, unknown>;

  // Check for explicit retry-after field
  if (typeof obj.retryAfter === "number") {
    return obj.retryAfter;
  }

  if (typeof obj["retry-after"] === "number") {
    return obj["retry-after"];
  }

  // Check for headers in response
  const response = obj.response as Record<string, unknown> | undefined;
  if (response && typeof response === "object") {
    const headers = response.headers as Record<string, unknown> | undefined;
    if (headers && typeof headers === "object") {
      const retryAfterHeader = headers["retry-after"];
      if (typeof retryAfterHeader === "string") {
        const parsed = Number.parseInt(retryAfterHeader, 10);
        if (!Number.isNaN(parsed)) {
          return parsed;
        }
      }
    }
  }

  // Try to extract from error message
  const errorStr = extractErrorString(error);
  const match = /(?:retry|wait)\s*(?:after|in)?\s*(\d+)\s*s/i.exec(errorStr);
  if (match?.[1]) {
    return Number.parseInt(match[1], 10);
  }

  return undefined;
}

/**
 * Checks if a string matches any of the patterns.
 */
function matchesPatterns(str: string, patterns: readonly string[]): boolean {
  return patterns.some((pattern) => str.includes(pattern));
}

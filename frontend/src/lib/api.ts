import { z } from "zod";

/**
 * Typed API client for the Go backend.
 *
 * Every response is validated with a Zod schema at the boundary: the network
 * is untrusted input even when it's our own API, and runtime validation is
 * what makes the TypeScript types true rather than aspirational.
 */

/** RFC 7807 problem details — the API's single error contract. */
export const problemSchema = z.object({
  type: z.string(),
  title: z.string(),
  status: z.number(),
  detail: z.string().optional(),
  request_id: z.string().optional(),
});

export type Problem = z.infer<typeof problemSchema>;

export class ApiError extends Error {
  constructor(public readonly problem: Problem) {
    super(problem.title);
    this.name = "ApiError";
  }
}

const API_BASE = process.env.NEXT_PUBLIC_API_BASE ?? "/api/v1";

interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  signal?: AbortSignal;
}

export async function apiFetch<T>(
  path: string,
  schema: z.ZodType<T>,
  options: RequestOptions = {},
): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: options.method ?? "GET",
    headers: {
      ...(options.body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    signal: options.signal,
    // Session cookie auth: always same-origin, never cross-site.
    credentials: "same-origin",
  });

  if (!response.ok) {
    const problem = problemSchema.safeParse(await response.json().catch(() => null));
    throw new ApiError(
      problem.success
        ? problem.data
        : { type: "about:blank", title: "Request failed", status: response.status },
    );
  }

  return schema.parse(await response.json());
}

/** Health endpoint schema — the first real contract with the backend. */
export const healthSchema = z.object({ status: z.string() });

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch("/api" + path, {
    method,
    headers: { "Content-Type": "application/json", "X-Reader-Request": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const value = await response
    .json()
    .catch(() => ({ error: "服务暂时不可用" }));
  if (!response.ok)
    throw new ApiError(value.error || "请求失败", response.status);
  return value as T;
}
export const audioURL = (id: string) => `/api/audio/${id}?format=wav`;

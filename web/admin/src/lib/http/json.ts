export async function readJsonOrNull<T = unknown>(response: Response): Promise<T | null> {
  const contentType = response.headers.get('content-type') || '';
  if (!contentType.includes('application/json')) return null;
  return response.json().catch(() => null);
}

export async function readJsonArray<T = unknown>(response: Response): Promise<T[]> {
  if (!response.ok) return [];
  const data = await readJsonOrNull(response);
  return Array.isArray(data) ? data : [];
}

export async function readErrorMessage(response: Response, fallback: string): Promise<string> {
  const data = await readJsonOrNull<{ error?: string; message?: string }>(response);
  return data?.error || data?.message || fallback;
}

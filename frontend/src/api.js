export async function apiRequest(path, { getToken, method = 'GET', body } = {}) {
  const headers = {};
  if (getToken) {
    const token = await getToken();
    if (token) headers.authorization = `Bearer ${token}`;
  }
  if (body !== undefined) headers['content-type'] = 'application/json';

  const response = await fetch(path, {
    method,
    headers,
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  if (response.status === 204) return null;

  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(payload?.error?.message ?? 'Não foi possível concluir a operação');
  }
  return payload;
}
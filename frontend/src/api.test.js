import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiRequest } from './api.js';

afterEach(() => vi.unstubAllGlobals());

describe('apiRequest', () => {
  it('sends the access token and JSON body to the BFF', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ id: 'link-id' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const result = await apiRequest('/api/links', {
      method: 'POST',
      getToken: async () => 'access-token',
      body: { targetUrl: 'https://example.com' },
    });

    expect(fetchMock).toHaveBeenCalledWith('/api/links', {
      method: 'POST',
      headers: {
        authorization: 'Bearer access-token',
        'content-type': 'application/json',
      },
      body: JSON.stringify({ targetUrl: 'https://example.com' }),
    });
    expect(result).toEqual({ id: 'link-id' });
  });

  it('returns null for an empty successful response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 204 }));

    await expect(apiRequest('/api/links/link-id', { method: 'DELETE' })).resolves.toBeNull();
  });

  it('surfaces the standard API error message', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: { message: 'URL inválida' } }),
    }));

    await expect(apiRequest('/api/links')).rejects.toThrow('URL inválida');
  });
});
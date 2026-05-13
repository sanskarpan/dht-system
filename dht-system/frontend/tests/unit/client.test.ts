import { afterEach, describe, expect, it, vi } from 'vitest';

import { getNetworkState, summarizeErrorResponse } from '../../src/api/client';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('summarizeErrorResponse', () => {
  it('extracts the useful message from JSON error payloads', () => {
    const summary = summarizeErrorResponse(
      'application/json',
      JSON.stringify({
        error: 'validation failed',
        details: 'ignored in favor of the primary error field',
      })
    );

    expect(summary).toBe('validation failed');
  });

  it('summarizes HTML responses without dumping the full document', () => {
    const summary = summarizeErrorResponse(
      'text/html',
      '<!doctype html><html><head><title>Gateway Error</title></head><body><h1>502</h1></body></html>'
    );

    expect(summary).toBe('HTML response: Gateway Error');
  });

  it('compacts plain-text responses into a readable preview', () => {
    const summary = summarizeErrorResponse('text/plain', '   upstream    timeout\n\n');

    expect(summary).toBe('upstream timeout');
  });
});

describe('request error handling', () => {
  it('throws ApiRequestError with a summarized HTML failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('<!doctype html><html><head><title>Proxy Error</title></head></html>', {
          status: 502,
          statusText: 'Bad Gateway',
          headers: {
            'content-type': 'text/html; charset=utf-8',
          },
        })
      )
    );

    await expect(getNetworkState()).rejects.toMatchObject({
      name: 'ApiRequestError',
      status: 502,
      statusText: 'Bad Gateway',
      contentType: 'text/html; charset=utf-8',
      summary: 'HTML response: Proxy Error',
      message: '502 Bad Gateway: HTML response: Proxy Error',
    });
  });

  it('throws ApiRequestError for JSON responses with a useful summary', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ message: 'spawn-node requires a valid addr' }), {
          status: 400,
          statusText: 'Bad Request',
          headers: {
            'content-type': 'application/json',
          },
        })
      )
    );

    await expect(getNetworkState()).rejects.toMatchObject({
      name: 'ApiRequestError',
      status: 400,
      summary: 'spawn-node requires a valid addr',
    });
  });
});

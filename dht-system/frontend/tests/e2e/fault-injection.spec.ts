import { expect, test, type Page } from '@playwright/test';

async function dismissTutorial(page: Page) {
  const skipTour = page.getByRole('button', { name: 'Skip tour' });
  if (await skipTour.count()) {
    await expect(skipTour).toBeVisible();
    await skipTour.click();
    await expect(skipTour).toHaveCount(0);
  }
}

test.describe('Fault injection matrix', () => {
  test('covers split, heal, and link latency controls from the browser', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.removeItem('dht-tutorial-done');
    });

    await page.goto('/');
    await dismissTutorial(page);

    await page.getByRole('button', { name: 'Split 50/50' }).click();
    await expect(page.getByText('Network partition applied')).toBeVisible();

    const splitFaults = await page.request.get('/api/v1/network/faults').then((res) => res.json());
    expect(splitFaults.partitions).toHaveLength(2);
    expect(splitFaults.links).toHaveLength(0);

    await page.getByRole('button', { name: 'Heal' }).click();
    await expect(page.getByText('Network partition healed')).toBeVisible();

    const healedFaults = await page.request.get('/api/v1/network/faults').then((res) => res.json());
    expect(healedFaults.partitions ?? []).toHaveLength(0);

    const state = await page.request.get('/api/v1/network/state').then((res) => res.json());
    expect(state.nodes.length).toBeGreaterThanOrEqual(2);
    const [from, to] = state.nodes.slice(0, 2).map((node: { addr: string }) => node.addr);

    const linkResponse = await page.request.put('/api/v1/network/links', {
      data: { from, to, latencyMs: 25 },
    });
    expect(linkResponse.ok()).toBeTruthy();

    const faultsWithLink = await page.request.get('/api/v1/network/faults').then((res) => res.json());
    expect(faultsWithLink.links).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ from, to, latencyMs: 25 }),
      ])
    );

    await page.request.delete('/api/v1/network/links');
    const clearedFaults = await page.request.get('/api/v1/network/faults').then((res) => res.json());
    expect(clearedFaults.links).toHaveLength(0);
  });
});

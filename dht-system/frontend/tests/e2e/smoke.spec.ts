import { expect, test } from '@playwright/test';

test.describe('DHT smoke suite', () => {
  test('covers the shipped UI and core gateway flows', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByRole('button', { name: 'Skip tour' })).toBeVisible();
    await page.getByRole('button', { name: 'Skip tour' }).click();
    await expect(page.getByRole('button', { name: 'Skip tour' })).toHaveCount(0);

    const suffix = Date.now().toString(36);
    const key = `e2e-${suffix}`;
    const value = `value-${suffix}`;

    await page.getByRole('textbox', { name: 'Key', exact: true }).fill(key);
    await page.getByRole('textbox', { name: 'Value', exact: true }).fill(value);
    await page.getByRole('button', { name: 'Insert' }).click();
    await expect(page.getByText('Key inserted successfully')).toBeVisible();

    await page.getByPlaceholder('Key to look up').fill(key);
    await page.getByRole('button', { name: 'Trace Lookup' }).click();
    await expect(page.getByText(/hops →/)).toBeVisible();

    await page.getByRole('button', { name: 'Split 50/50' }).click();
    await expect(page.getByText('Network partition applied')).toBeVisible();

    await page.getByRole('button', { name: 'Heal' }).click();
    await expect(page.getByText('Network partition healed')).toBeVisible();
  });
});

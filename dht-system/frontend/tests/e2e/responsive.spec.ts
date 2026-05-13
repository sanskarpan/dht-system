import { expect, test, type Page } from '@playwright/test';

const viewports = [
  { name: 'mobile', width: 390, height: 844 },
  { name: 'tablet', width: 820, height: 1180 },
];

const routes = [
  {
    path: '/',
    heading: null,
    control: /Add Node/,
  },
  {
    path: '/lookup',
    heading: 'Lookup Tracer',
  },
  {
    path: '/consistency',
    heading: 'Consistency Dashboard',
  },
  {
    path: '/metrics-ui',
    heading: 'Performance Metrics',
  },
  {
    path: '/scenarios',
    heading: 'Scenario Runner',
  },
] as const;

async function dismissTutorial(page: Page) {
  const skipTour = page.getByRole('button', { name: 'Skip tour' });
  if (await skipTour.count()) {
    await expect(skipTour).toBeVisible();
    await skipTour.click();
    await expect(skipTour).toHaveCount(0);
  }
}

async function expectNoHorizontalOverflow(page: Page) {
  const metrics = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.clientWidth + 1);
}

test.describe('Responsive layout audit', () => {
  for (const viewport of viewports) {
    test(`routes fit ${viewport.name} widths`, async ({ page }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });

      for (const route of routes) {
        await page.addInitScript(() => {
          localStorage.removeItem('dht-tutorial-done');
        });

        await page.goto(route.path);
        await dismissTutorial(page);

        if (route.heading) {
          await expect(page.getByRole('heading', { name: route.heading })).toBeVisible();
        }
        if ('control' in route) {
          await expect(page.getByRole('button', { name: route.control })).toBeVisible();
        }
        await expectNoHorizontalOverflow(page);
      }
    });
  }
});

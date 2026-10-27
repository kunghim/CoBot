import { test, expect } from '@playwright/test';
import { truncateSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, selectWorkspaceFile } from './helpers';

test.beforeEach(async ({ context }) => { await authorize(context); });
test('notebooks page cells, retain saved outputs and reject invalid or oversized input without execution', async ({ page }) => {
  await page.goto('/chat/new');
  const response = page.waitForResponse((item) => item.url().endsWith('/api/v1/chats') && item.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('notebook matrix setup');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await response).json()).data;
  await expect(page.getByText('收到：notebook matrix setup', { exact: true })).toBeVisible();
  const cells = Array.from({ length: 51 }, (_, index) => ({ cell_type: 'code', source: 'CELL-' + index, execution_count: null, outputs: [] as unknown[] }));
  cells[0] = { cell_type: 'code', source: 'document.body.dataset.notebookMatrixAttack="executed"', execution_count: null, outputs: [
    { output_type: 'stream', text: 'Saved stream output' },
    { output_type: 'error', ename: 'FixtureError', evalue: 'Saved error output', traceback: [] },
    { output_type: 'display_data', data: { 'text/html': '<h1>Saved HTML output</h1><script>parent.document.body.dataset.notebookMatrixAttack="html"</script><img src="https://notebook-matrix.invalid/blocked"><style>@import "https://notebook-matrix.invalid/style";</style>' } },
    { output_type: 'display_data', data: { 'image/svg+xml': '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="12"><script>parent.document.body.dataset.notebookMatrixAttack="svg"</script><rect width="16" height="12" fill="blue"/></svg>' } },
  ] };
  const write = (name: string, data: unknown) => writeFileSync(join(created.chat.workspace, name), JSON.stringify(data));
  write('paged.ipynb', { nbformat: 4, cells });
  write('blank.ipynb', { nbformat: 4, cells: [] });
  write('unversioned.ipynb', { cells: [{ cell_type: 'raw', source: 'Saved cell without version marker' }] });
  write('old.ipynb', { nbformat: 3, worksheets: [] });
  write('invalid-cell.ipynb', { nbformat: 4, cells: [{ cell_type: 'unknown', source: 'bad' }] });
  write('too-many-cells.ipynb', { nbformat: 4, cells: Array.from({ length: 10001 }, () => ({ cell_type: 'raw', source: '' })) });
  writeFileSync(join(created.chat.workspace, 'empty.ipynb'), '');
  const oversized = join(created.chat.workspace, 'oversized.ipynb');
  writeFileSync(oversized, ''); truncateSync(oversized, 11 * 1024 * 1024);
  const content: string[] = [], external: string[] = [], blocked: string[] = [], responses: string[] = [];
  page.on('request', (request) => { const url = new URL(request.url()); if (url.pathname.endsWith('/content')) content.push(url.searchParams.get('path') ?? ''); if (url.hostname === 'notebook-matrix.invalid') external.push(url.href); });
  page.on('requestfailed', (request) => { if (new URL(request.url()).hostname === 'notebook-matrix.invalid') blocked.push(request.failure()?.errorText ?? ''); });
  page.on('response', (response) => { if (new URL(response.url()).hostname === 'notebook-matrix.invalid') responses.push(response.url()); });
  await page.getByRole('button', { name: '创建标签页', exact: true }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  await selectWorkspaceFile(page, 'paged.ipynb');
  const workspace = page.getByRole('tabpanel', { name: '工作区' });
  await expect(workspace.locator('section[aria-label$="个单元"]')).toHaveCount(50);
  await expect(workspace.getByText('Saved stream output', { exact: true })).toBeVisible();
  await expect(workspace.getByText('FixtureError: Saved error output', { exact: true })).toBeVisible();
  await expect(page.frameLocator('iframe[title="第 3 个输出"]').getByRole('heading', { name: 'Saved HTML output' })).toBeVisible();
  await expect(page.locator('iframe[title="第 3 个输出"]')).toHaveAttribute('sandbox', '');
  await expect.poll(() => workspace.getByRole('img', { name: '第 4 个输出' }).evaluate((image: HTMLImageElement) => [image.naturalWidth, image.naturalHeight])).toEqual([16, 12]);
  expect(await page.locator('body').getAttribute('data-notebook-matrix-attack')).toBeNull();
  // Chromium reports a request event even when CSP blocks it before sending.
  await expect.poll(() => blocked.length).toBe(external.length);
  expect(blocked.every((reason) => /csp/i.test(reason))).toBe(true);
  expect(responses).toEqual([]);
  await workspace.getByRole('button', { name: '下一页', exact: true }).click();
  await expect(workspace.locator('section[aria-label$="个单元"]')).toHaveCount(1);
  await expect(workspace.getByText('CELL-50', { exact: true })).toBeVisible();
  await workspace.getByRole('button', { name: '上一页', exact: true }).click();
  await expect(workspace.locator('section[aria-label$="个单元"]')).toHaveCount(50);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-notebook-matrix.png') });
  await selectWorkspaceFile(page, 'blank.ipynb');
  await expect(workspace.getByText('此笔记本没有单元。', { exact: true })).toBeVisible();
  await selectWorkspaceFile(page, 'unversioned.ipynb');
  await expect(workspace.getByText('Saved cell without version marker', { exact: true })).toBeVisible();
  for (const name of ['old.ipynb', 'invalid-cell.ipynb', 'too-many-cells.ipynb', 'empty.ipynb']) {
    await selectWorkspaceFile(page, name);
    await expect(workspace.getByText('此笔记本已损坏，或使用了不支持的格式。', { exact: true })).toBeVisible();
  }
  await selectWorkspaceFile(page, 'oversized.ipynb');
  await expect(workspace.getByText('此文件较大，请切换源码分页查看完整内容。', { exact: true })).toBeVisible();
  expect(content).not.toContain('oversized.ipynb');
  await expect.poll(() => blocked.length).toBe(external.length);
  expect(blocked.every((reason) => /csp/i.test(reason))).toBe(true);
  expect(responses).toEqual([]);
});

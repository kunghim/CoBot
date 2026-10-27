import { test, expect } from '@playwright/test';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

// The workspace beside the conversation: the new-tab button opens it on its
// new-tab page, the user can open a browser page of their own, Esc closes
// the info card with its focus back on the button, and closing the last tab
// takes the workspace down and brings the button back.
test('workspace opens with tabs, the info card closes on Escape', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'workspace-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('reply', teamDir);

  await page.goto('/chat/new');
  await page.getByRole('radio', { name: '团队', exact: true }).click();
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill('工作区团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await expect(page.getByText('工作区团队').first()).toBeVisible({ timeout: 15000 });
  const leaders = page.getByRole('button', { name: /项目经理/ }).filter({ visible: true });
  await expect(leaders).toHaveCount(1, { timeout: 15000 });
  await leaders.first().click();

  // The new-tab button opens the workspace on its new-tab page, and stands
  // aside while the workspace is open.
  const create = page.getByRole('button', { name: '创建标签页' });
  await create.click();
  const workspace = page.getByRole('tablist', { name: '工作区' });
  await expect(workspace).toBeVisible();
  await expect(workspace.getByRole('tab', { name: '新建标签页' })).toHaveAttribute('aria-selected', 'true');
  await expect(create).toHaveCount(0);

  // The info card floats over the conversation; Escape closes it and the
  // focus returns to the button that opened it.
  const info = page.getByRole('button', { name: '展开信息面板' });
  await info.click();
  const card = page.getByRole('region', { name: '信息面板' });
  await expect(card).toBeVisible();
  await expect(card.getByRole('tablist', { name: '工作面板页面' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(card).toHaveCount(0);
  await expect(info).toBeFocused();

  // A browser page of the user's own: the new-tab page opens one, the tab
  // shows the page, the viewport row and the shared status line. The
  // browser takes a moment to come up.
  await page.getByRole('button', { name: /打开一个网页标签/ }).click();
  await expect(workspace.getByRole('tab', { name: /空白页|about:blank/ })).toHaveAttribute('aria-selected', 'true', { timeout: 20000 });
  await expect(page.getByText('成员需要时可以查看')).toBeVisible({ timeout: 20000 });
  await expect(page.getByRole('combobox', { name: /视口大小/ })).toBeVisible();

  // Closing the last tab takes the workspace down; the button returns.
  await workspace.getByRole('button', { name: /关闭 (空白页|about:blank)/ }).click();
  await expect(workspace).toHaveCount(0);
  await expect(page.getByRole('button', { name: '创建标签页' })).toBeVisible();
});


test('file tree reads, navigates, restores and adapts to a narrow workspace', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'files-' + Date.now());
  mkdirSync(join(teamDir, 'src'), { recursive: true });
  mkdirSync(join(teamDir, 'empty'));
  mkdirSync(join(teamDir, 'docs'));
  writeFileSync(join(teamDir, 'docs', 'readme.md'), '# Relative document\n\n[Go to a](../src/a.ts:1:7)\n\n![fixture image](picture.svg)\n');
  writeFileSync(join(teamDir, 'docs', 'picture.svg'), '<svg xmlns="http://www.w3.org/2000/svg" width="32" height="24"><rect width="32" height="24" fill="green"/></svg>');
  writeFileSync(join(teamDir, 'src', 'a.ts'), 'const firstFile = 1;\n');
  writeFileSync(join(teamDir, 'src', 'b.ts'), 'const secondFile = 2;\n');
  writeFileSync(join(teamDir, 'src', 'long.ts'), Array.from({ length: 700 }, (_, index) => index === 650 ? 'const uniqueNeedle = 651;' : `// line ${index + 1}`).join('\n'));
  writeFileSync(join(teamDir, '.hidden'), 'hidden fixture');
  const wav = Buffer.alloc(44 + 16000);
  wav.write('RIFF', 0); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8); wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22); wav.writeUInt32LE(8000, 24); wav.writeUInt32LE(16000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(16000, 40);
  writeFileSync(join(teamDir, 'docs', 'tone.wav'), wav);
  writeFileSync(join(teamDir, 'docs', 'broken.mp3'), 'invalid audio fixture');
  writeFileSync(join(teamDir, 'docs', 'sample.ipynb'), JSON.stringify({ nbformat: 4, cells: [
    { cell_type: 'markdown', source: ['# Notebook fixture\n', '[Open source](../src/a.ts)\n\n![Embedded image](attachment:pixel.png)'], attachments: { 'pixel.png': { 'image/png': 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aDXsAAAAASUVORK5CYII=' } } },
    { cell_type: 'code', source: 'print(42)', execution_count: 1, outputs: [{ output_type: 'stream', text: '42\n' }, { output_type: 'display_data', data: { 'text/html': '<script>parent.document.body.dataset.notebookAttack="yes"</script><table><tr><td>Safe result</td></tr></table><img src="https://notebook-test.invalid/blocked">' } }] },
  ] }));
  writeFileSync(join(teamDir, 'docs', 'broken.ipynb'), '{');
  mkdirSync(join(teamDir, 'docs', 'styles'));
  writeFileSync(join(teamDir, 'docs', 'styles', 'site.css'), '@import "theme.css"; .preview-page{padding:12px;background-image:url(../picture.svg)}');
  const fontFixture = process.env.COBOT_E2E_FONT_FIXTURE;
  if (fontFixture) writeFileSync(join(teamDir, 'docs', 'fixture.ttf'), readFileSync(fontFixture));
  writeFileSync(join(teamDir, 'docs', 'styles', 'theme.css'), '.preview-page{color:rgb(12,34,56)}' + (fontFixture ? '@font-face{font-family:PreviewFixture;src:url(../fixture.ttf)}.preview-page{font-family:PreviewFixture}' : ''));
  writeFileSync(join(teamDir, 'docs', 'page.html'), '<!doctype html><html lang="zh"><head><link rel="stylesheet" href="styles/site.css"></head><body class="preview-page"><h1>Scoped HTML preview</h1><img alt="Scoped image" src="picture.svg"><script>parent.document.body.dataset.htmlAttack="yes"</script><a href="https://example.com">Disabled navigation</a><form><input name="secret"></form></body></html>');
  writeFileSync(join(teamDir, 'docs', 'missing.html'), '<h1>Missing resource fixture</h1><img src="../../../../outside.png"><img src="https://html-preview-test.invalid/tracker">');
  const streams = ['BT /F1 16 Tf 20 100 Td (PDF first page) Tj ET', 'BT /F1 16 Tf 20 100 Td (PDF second page) Tj ET'];
  const objects = ['<< /Type /Catalog /Pages 2 0 R >>', '<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 7 0 R >> >> /Contents 4 0 R >>',
    `<< /Length ${streams[0].length} >>\nstream\n${streams[0]}\nendstream`,
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>',
    `<< /Length ${streams[1].length} >>\nstream\n${streams[1]}\nendstream`, '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'];
  let pdf = '%PDF-1.7\n'; const offsets = [0];
  objects.forEach((object, index) => { offsets.push(Buffer.byteLength(pdf)); pdf += `${index + 1} 0 obj\n${object}\nendobj\n`; });
  pdf += '%' + 'padding'.repeat(12000) + '\n';
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 8\n0000000000 65535 f \n${offsets.slice(1).map((offset) => String(offset).padStart(10, '0') + ' 00000 n \n').join('')}trailer\n<< /Size 8 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  writeFileSync(join(teamDir, 'docs', 'sample.pdf'), pdf);
  writeFileSync(join(teamDir, 'docs', 'retry.pdf'), pdf);
  writeFileSync(join(teamDir, 'docs', 'broken.pdf'), 'invalid PDF fixture');
  const encryptedPDF = process.env.COBOT_E2E_ENCRYPTED_PDF;
  if (encryptedPDF) writeFileSync(join(teamDir, 'docs', 'encrypted.pdf'), readFileSync(encryptedPDF));



  setEngineMode('reply', teamDir);
  await page.goto('/chat/new');
  const videoBytes = await page.evaluate(async () => {
    const canvas = document.createElement('canvas'); canvas.width = 16; canvas.height = 16;
    const context = canvas.getContext('2d')!;
    const stream = canvas.captureStream(5);
    const recorder = new MediaRecorder(stream, { mimeType: 'video/webm;codecs=vp8' });
    const chunks: Blob[] = [];
    recorder.ondataavailable = (event) => chunks.push(event.data);
    const finished = new Promise<number[]>((resolve) => { recorder.onstop = async () => resolve([...new Uint8Array(await new Blob(chunks, { type: 'video/webm' }).arrayBuffer())]); });
    recorder.start();
    let frame = 0;
    const timer = setInterval(() => { context.fillStyle = frame++ % 2 ? 'green' : 'blue'; context.fillRect(0, 0, 16, 16); }, 50);
    setTimeout(() => { clearInterval(timer); recorder.stop(); stream.getTracks().forEach((track) => track.stop()); }, 1000);
    return finished;
  });
  writeFileSync(join(teamDir, 'docs', 'sample.webm'), new Uint8Array(videoBytes));

  await page.getByRole('radio', { name: '团队', exact: true }).click();
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill('文件工作区团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await page.getByRole('button', { name: /项目经理/ }).filter({ visible: true }).first().click();
  await page.getByRole('button', { name: '创建标签页' }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  const tree = page.getByRole('tree', { name: '项目文件' });
  await expect(tree).toBeVisible();
  await expect(tree.getByRole('treeitem', { name: '.hidden', exact: true })).toHaveCount(0);
  await tree.getByRole('treeitem').filter({ hasText: /^src$/ }).click();
  await tree.getByRole('treeitem', { name: 'a.ts', exact: true }).click();
  const files = page.getByRole('tabpanel', { name: '工作区' });
  await expect(files.getByText('const firstFile = 1;', { exact: true })).toBeVisible();
  await expect(files.locator('.hljs-keyword').filter({ hasText: 'const' })).toBeVisible();
  await tree.getByRole('treeitem', { name: 'b.ts', exact: true }).click();
  await expect(files.getByText('const secondFile = 2;', { exact: true })).toBeVisible();
  await page.getByRole('checkbox', { name: '显示隐藏文件' }).check();
  await expect(tree.getByRole('treeitem', { name: '.hidden', exact: true })).toBeVisible();
  await page.reload();
  const team = page.getByRole('button', { name: /文件工作区团队/ }).first();
  if ((await team.getAttribute('aria-expanded')) !== 'true') await team.click();
  await page.getByRole('button', { name: /项目经理/ }).filter({ visible: true }).first().click();
  await expect(files.getByText('const secondFile = 2;', { exact: true })).toBeVisible();
  await expect(tree.getByRole('treeitem', { name: 'b.ts', exact: true })).toBeVisible();
  await tree.getByRole('treeitem').filter({ hasText: /^docs$/ }).click();
  await tree.getByRole('treeitem', { name: 'readme.md', exact: true }).click();
  await expect(files.getByRole('heading', { name: 'Relative document', exact: true })).toBeVisible();
  await expect(files.getByRole('img', { name: 'fixture image' })).toHaveJSProperty('naturalWidth', 32);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-markdown-wide.png') });
  await files.getByRole('link', { name: 'Go to a', exact: true }).click();
  await expect(files.getByText('const firstFile = 1;', { exact: true })).toBeVisible();
  await expect(files.locator('[data-line="1"]')).toHaveClass(/bg-signal/);
  await expect(page.getByRole('tab', { name: 'a.ts', exact: true })).toHaveCount(1);
  await tree.getByRole('treeitem', { name: 'long.ts', exact: true }).click();
  await expect(files.getByText('// line 1', { exact: true })).toBeVisible();
  await files.getByRole('textbox', { name: '在文件中查找（区分大小写）', exact: true }).fill('uniqueNeedle');
  await expect(files.getByText('1 个结果', { exact: true })).toBeVisible();
  await files.getByRole('button', { name: '定位第 1 个', exact: true }).click();
  await expect(files.locator('[data-line="651"]')).toHaveClass(/bg-signal/);
  await expect(files.getByText('uniqueNeedle', { exact: true })).toBeVisible();
  await tree.getByRole('treeitem').filter({ hasText: /^docs$/ }).click();
  await tree.getByRole('treeitem', { name: 'picture.svg', exact: true }).click();
  const picture = files.getByRole('img', { name: 'picture.svg', exact: true });
  await expect(picture).toHaveJSProperty('naturalWidth', 32);
  await files.getByRole('button', { name: '100%', exact: true }).click();
  await files.getByRole('button', { name: '放大', exact: true }).click();
  await expect(picture).toHaveCSS('width', '40px');
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-image-zoom.png') });
  await files.getByRole('button', { name: '适应窗口', exact: true }).click();
  await tree.getByRole('treeitem', { name: 'tone.wav', exact: true }).click();
  const audio = files.locator('audio');
  await expect.poll(() => audio.evaluate((element) => element.duration)).toBe(1);
  await audio.evaluate(async (element) => { await element.play(); element.pause(); element.currentTime = 0.5; });
  await expect.poll(() => audio.evaluate((element) => element.currentTime)).toBe(0.5);

  await tree.getByRole('treeitem', { name: 'sample.webm', exact: true }).click();
  const video = files.locator('video');
  await expect.poll(() => video.evaluate((element) => element.videoWidth)).toBe(16);
  await video.evaluate(async (element) => { await element.play(); element.pause(); });
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-video.png') });
  await tree.getByRole('treeitem', { name: 'broken.mp3', exact: true }).click();
  await expect(files.getByText('此音视频文件已损坏，或使用了不支持的编码。', { exact: true })).toBeVisible();
  await tree.getByRole('treeitem', { name: 'sample.ipynb', exact: true }).click();
  await expect(files.getByRole('heading', { name: 'Notebook fixture', exact: true })).toBeVisible();
  await expect(files.getByRole('img', { name: 'Embedded image', exact: true })).toHaveJSProperty('naturalWidth', 1);
  await expect(files.getByText('print(42)', { exact: true })).toBeVisible();
  await expect(files.frameLocator('iframe').getByRole('cell', { name: 'Safe result', exact: true })).toBeVisible();
  await expect(page.locator('body')).not.toHaveAttribute('data-notebook-attack');
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-notebook.png') });
  await files.getByRole('button', { name: '源码', exact: true }).click();
  await expect(files.getByRole('textbox', { name: '在文件中查找（区分大小写）', exact: true })).toBeVisible();
  await tree.getByRole('treeitem', { name: 'broken.ipynb', exact: true }).click();
  await expect(files.getByText('此笔记本已损坏，或使用了不支持的格式。', { exact: true })).toBeVisible();
  await tree.getByRole('treeitem', { name: 'page.html', exact: true }).click();
  const htmlFrame = files.frameLocator('iframe');
  await expect(htmlFrame.getByRole('heading', { name: 'Scoped HTML preview', exact: true })).toBeVisible();
  await expect(htmlFrame.getByRole('heading', { name: 'Scoped HTML preview', exact: true })).toHaveCSS('color', 'rgb(12, 34, 56)');
  await expect(htmlFrame.getByRole('img', { name: 'Scoped image', exact: true })).toHaveJSProperty('naturalWidth', 32);
  await expect(htmlFrame.locator('body')).toHaveClass('preview-page');
  if (fontFixture) await expect.poll(() => htmlFrame.locator('body').evaluate(async (element) => (await element.ownerDocument.fonts.load('16px PreviewFixture')).filter((font) => font.status === 'loaded').length)).toBe(1);
  await expect(htmlFrame.locator('script,form,a[href]')).toHaveCount(0);
  await expect(files.getByText('静态预览 · 脚本和页面导航已禁用。', { exact: true })).toBeVisible();
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-html.png') });
  await files.getByRole('button', { name: '源码', exact: true }).click();
  await expect(files.getByRole('textbox', { name: '在文件中查找（区分大小写）', exact: true })).toBeVisible();
  await tree.getByRole('treeitem', { name: 'missing.html', exact: true }).click();
  await expect(files.getByText('静态预览 · 部分本地资源无法加载。', { exact: true })).toBeVisible();
  await expect(files.frameLocator('iframe').locator('img[src]')).toHaveCount(0);
  const pdfRanges: string[] = [];
  const pdfFonts: string[] = [];
  page.on('request', (request) => {
    if (new URL(request.url()).searchParams.get('path') === 'docs/sample.pdf' && request.headers().range) pdfRanges.push(request.headers().range);
    if (/\/assets\/pdfjs\/[^/]+\/standard_fonts\//.test(request.url())) pdfFonts.push(request.url());
  });
  await tree.getByRole('treeitem', { name: 'sample.pdf', exact: true }).click();
  await expect(files.getByText('PDF first page', { exact: true })).toBeVisible();
  await expect(files.locator('[aria-busy="true"]')).toHaveCount(0);
  expect(pdfRanges.length).toBeGreaterThanOrEqual(2);
  await expect.poll(() => pdfFonts.length).toBeGreaterThan(0);
  const originalPDFWidth = await files.locator('canvas').evaluate((element) => element.width);
  await files.getByRole('button', { name: '放大', exact: true }).click();
  await expect.poll(() => files.locator('canvas').evaluate((element) => element.width)).toBeGreaterThan(originalPDFWidth);
  await files.getByRole('button', { name: '下一页', exact: true }).click();
  await expect(files.getByText('PDF second page', { exact: true })).toBeVisible();
  await files.getByRole('textbox', { name: '在 PDF 中查找', exact: true }).fill('PDF');
  await files.getByRole('button', { name: '查找', exact: true }).click();
  await expect(files.locator('.pdf-text-layer mark')).toHaveText('PDF');
  await expect(files.getByRole('spinbutton', { name: '页码', exact: true })).toHaveValue('1');
  await files.getByRole('button', { name: '下一个', exact: true }).click();
  await expect(files.getByRole('spinbutton', { name: '页码', exact: true })).toHaveValue('2');
  await expect(files.locator('.pdf-text-layer mark')).toHaveText('PDF');
  await files.getByRole('button', { name: '缩小', exact: true }).click();
  await expect(files.locator('.pdf-text-layer mark')).toBeVisible();
  await expect(files.locator('.pdf-text-layer mark')).toHaveCSS('background-color', 'rgba(250, 204, 21, 0.4)');
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-pdf-search.png') });
  await files.getByRole('textbox', { name: '在 PDF 中查找', exact: true }).fill('not in document');
  await files.getByRole('button', { name: '查找', exact: true }).click();
  await expect(files.getByRole('status').filter({ hasText: '0 / 0' })).toBeVisible();
  await expect(files.locator('.pdf-text-layer mark')).toHaveCount(0);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-pdf.png') });
  await tree.getByRole('treeitem', { name: 'broken.pdf', exact: true }).click();
  await expect(files.getByText('此 PDF 已损坏或无法显示。', { exact: true })).toBeVisible();
  let failPDFRead = true;
  await page.route((url) => url.pathname.endsWith('/content') && url.searchParams.get('path') === 'docs/retry.pdf', (route) => {
    if (failPDFRead) { failPDFRead = false; return route.abort('failed'); }
    return route.continue();
  });
  await tree.getByRole('treeitem', { name: 'retry.pdf', exact: true }).click();
  await expect(files.getByText('此 PDF 已损坏或无法显示。', { exact: true })).toBeVisible();
  await files.getByRole('button', { name: '刷新文件', exact: true }).first().click();
  await expect(files.getByText('PDF first page', { exact: true })).toBeVisible();

  if (encryptedPDF) {
    await tree.getByRole('treeitem', { name: 'encrypted.pdf', exact: true }).click();
    await files.getByLabel('请输入 PDF 密码。', { exact: true }).fill('wrong-password');
    await files.getByRole('button', { name: '打开 PDF', exact: true }).click();
    await expect(files.getByText('密码不正确，请重试。', { exact: true })).toBeVisible();
    await files.getByLabel('请输入 PDF 密码。', { exact: true }).fill('preview-password');
    await files.getByRole('button', { name: '打开 PDF', exact: true }).click();
    await expect(files.getByText('PDF first page', { exact: true })).toBeVisible();
  }





  await tree.getByRole('treeitem', { name: 'a.ts', exact: true }).click();
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-source-wide.png') });
  await page.locator('html').evaluate((element) => { element.dataset.mode = 'dark'; element.style.colorScheme = 'dark'; });
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-source-dark.png') });
  await page.locator('html').evaluate((element) => { element.dataset.mode = 'light'; element.style.colorScheme = 'light'; });
  await page.setViewportSize({ width: 440, height: 800 });
  await expect(page.getByRole('button', { name: '展开文件树', exact: true })).toBeVisible();
  await expect(files.getByText('const firstFile = 1;', { exact: true })).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-narrow.png') });
});

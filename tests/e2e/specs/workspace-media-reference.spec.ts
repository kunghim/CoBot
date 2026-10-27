import { test, expect } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join } from 'node:path';
import { authorize } from './helpers';

const directory = process.env.COBOT_E2E_EXTERNAL_MEDIA_DIR;
const samples = [
  ['bear-audio-10s-VBR-has-TOC.mp3', 'reference.mp3', 'audio', 10.018],
  ['sfx.ogg', 'reference.ogg', 'audio', 0.353],
  ['sfx.ogg', 'reference.oga', 'audio', 0.353],
  ['opus-test.opus', 'reference.opus', 'audio', 1.016],
  ['bear-320x240.webm', 'reference.webm', 'video', 2.744],
] as const;
test.beforeEach(async ({ context }) => { await authorize(context); });

test('reference MP3, Vorbis, Opus and indexed WebM play and seek through workspace tabs', async ({ page, browser }) => {
  test.skip(!directory, 'Requires explicitly supplied reference media with a download manifest');
  const manifest = JSON.parse(readFileSync(join(directory!, 'download-manifest.json'), 'utf8')) as Array<{ name: string; sha256: string }>;
  const bytes = (name: string) => {
    const data = readFileSync(join(directory!, name));
    expect(createHash('sha256').update(data).digest('hex')).toBe(manifest.find((item) => item.name === name)?.sha256);
    return data;
  };
  await page.goto('/chat/new');
  const response = page.waitForResponse((item) => item.url().endsWith('/api/v1/chats') && item.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('reference media setup');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await response).json()).data;
  await expect(page.getByText('收到：reference media setup', { exact: true })).toBeVisible();
  for (const [source, target] of samples) writeFileSync(join(created.chat.workspace, target), bytes(source));
  writeFileSync(join(created.chat.workspace, 'reference.ogv'), bytes('blackwhite_yuv420p.ogv'));
  await page.getByRole('button', { name: '创建标签页', exact: true }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  const workspace = page.getByRole('tabpanel', { name: '工作区' });
  const results: Array<{ file: string; outcome: string; duration?: number }> = [];
  for (const [, target, kind, expectedDuration] of samples) {
    await page.getByRole('treeitem', { name: target, exact: true }).click();
    const player = workspace.locator(`${kind}[aria-label="${target}"]`);
    await expect.poll(() => player.evaluate((element: HTMLMediaElement) => element.readyState)).toBeGreaterThanOrEqual(2);
    const duration = await player.evaluate((element: HTMLMediaElement) => element.duration);
    expect(Number.isFinite(duration)).toBe(true); expect(duration).toBeGreaterThan(0.2);
    expect(Math.abs(duration - expectedDuration)).toBeLessThan(0.1);
    expect(await player.evaluate((element: HTMLMediaElement) => element.controls && element.paused && !element.autoplay)).toBe(true);
    await player.evaluate(async (element: HTMLMediaElement) => { element.volume = 0; await element.play(); });
    await expect.poll(() => player.evaluate((element: HTMLMediaElement) => element.currentTime)).toBeGreaterThan(0.05);
    await player.evaluate((element: HTMLMediaElement) => { element.pause(); element.currentTime = element.duration / 2; });
    await expect.poll(() => player.evaluate((element: HTMLMediaElement) => Math.abs(element.currentTime - element.duration / 2))).toBeLessThan(0.05);
    results.push({ file: target, outcome: 'played-and-seeked', duration });
  }
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-reference-webm.png') });
  const theora = await page.evaluate(() => document.createElement('video').canPlayType('video/ogg;codecs=theora'));
  await page.getByRole('treeitem', { name: 'reference.ogv', exact: true }).click();
  if (!theora) {
    await expect(workspace.getByText('此音视频文件已损坏，或使用了不支持的编码。', { exact: true })).toBeVisible();
    results.push({ file: 'reference.ogv', outcome: 'platform-unsupported-with-visible-error' });
  } else {
    await expect.poll(() => workspace.locator('video').evaluate((element: HTMLVideoElement) => element.readyState)).toBeGreaterThanOrEqual(2);
    await workspace.locator('video').evaluate(async (element: HTMLVideoElement) => { element.volume = 0; await element.play(); element.pause(); });
    results.push({ file: 'reference.ogv', outcome: 'played' });
  }
  writeFileSync(join(process.env.COBOT_E2E_ROOT!, 'reference-media-result.json'), JSON.stringify({ browser: browser.version(), results }, null, 2) + '\n');
});

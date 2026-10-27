import { test, expect } from '@playwright/test';
import { readFileSync, truncateSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, selectWorkspaceFile } from './helpers';

const audio = JSON.parse(readFileSync(new URL('../fixtures/file-audio.json', import.meta.url), 'utf8')) as Array<{ extensions: string[]; codec: string; duration: number; data: string }>;
const video = JSON.parse(readFileSync(new URL('../fixtures/file-video.json', import.meta.url), 'utf8')) as { extension: string; width: number; height: number; data: string };
test.beforeEach(async ({ context }) => { await authorize(context); });

test('media decodes generated AAC, FLAC, MPEG-4 audio, PCM and H.264 with native controls', async ({ page }) => {
  await page.goto('/chat/new');
  const response = page.waitForResponse((item) => item.url().endsWith('/api/v1/chats') && item.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('media matrix setup');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await response).json()).data;
  await expect(page.getByText('收到：media matrix setup', { exact: true })).toBeVisible();
  const names: string[] = [];
  for (const sample of audio) for (const extension of sample.extensions) {
    const name = 'tone.' + extension; names.push(name);
    writeFileSync(join(created.chat.workspace, name), Buffer.from(sample.data, 'base64'));
  }
  const wav = Buffer.alloc(44 + 16000);
  wav.write('RIFF', 0); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8); wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22); wav.writeUInt32LE(8000, 24); wav.writeUInt32LE(16000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(16000, 40);
  writeFileSync(join(created.chat.workspace, 'tone.wav'), wav); names.push('tone.wav');
  writeFileSync(join(created.chat.workspace, 'clip.' + video.extension), Buffer.from(video.data, 'base64'));
  const extensions = ['aac', 'flac', 'm4a', 'mp3', 'mp4a', 'oga', 'ogg', 'opus', 'wav', 'mp4', 'ogv', 'webm'];
  for (const extension of extensions) {
    writeFileSync(join(created.chat.workspace, 'damaged.' + extension), 'not media');
    writeFileSync(join(created.chat.workspace, 'empty.' + extension), '');
  }
  for (const extension of ['wav', 'mp4']) {
    const path = join(created.chat.workspace, 'oversized.' + extension);
    writeFileSync(path, ''); truncateSync(path, 129 * 1024 * 1024);
  }
  const content: string[] = [];
  page.on('request', (request) => { const url = new URL(request.url()); if (url.pathname.endsWith('/content')) content.push(url.searchParams.get('path') ?? ''); });
  await page.getByRole('button', { name: '创建标签页', exact: true }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  const workspace = page.getByRole('tabpanel', { name: '工作区' });
  for (const name of [...names, 'clip.' + video.extension]) {
    await selectWorkspaceFile(page, name);
    const player = workspace.locator(`${name.startsWith('clip.') ? 'video' : 'audio'}[aria-label="${name}"]`);
    await expect.poll(() => player.evaluate((element: HTMLMediaElement) => element.readyState)).toBeGreaterThanOrEqual(2);
    expect(await player.evaluate((element: HTMLMediaElement) => element.controls && element.paused && !element.autoplay)).toBe(true);
    const duration = await player.evaluate((element: HTMLMediaElement) => element.duration);
    expect(Number.isFinite(duration)).toBe(true); expect(duration).toBeGreaterThan(0.8); expect(duration).toBeLessThan(1.5);
    await player.evaluate(async (element: HTMLMediaElement) => { element.volume = 0.25; await element.play(); });
    await expect.poll(() => player.evaluate((element: HTMLMediaElement) => element.currentTime)).toBeGreaterThan(0.1);
    await player.evaluate((element: HTMLMediaElement) => { element.pause(); element.currentTime = 0.5; });
    await expect.poll(() => player.evaluate((element: HTMLMediaElement) => Math.abs(element.currentTime - 0.5))).toBeLessThan(0.05);
    expect(await player.evaluate((element: HTMLMediaElement) => element.volume)).toBe(0.25);
    if (name.startsWith('clip.')) {
      expect(await player.evaluate((element: HTMLVideoElement) => [element.videoWidth, element.videoHeight])).toEqual([video.width, video.height]);
      expect(await player.evaluate((element) => element.getBoundingClientRect().width)).toBeGreaterThan(200);
    }
  }
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-media.png') });
  for (const extension of extensions) for (const prefix of ['damaged', 'empty']) {
    await selectWorkspaceFile(page, prefix + '.' + extension);
    await expect(workspace.getByText('此音视频文件已损坏，或使用了不支持的编码。', { exact: true })).toBeVisible();
  }
  for (const extension of ['wav', 'mp4']) {
    const name = 'oversized.' + extension;
    await selectWorkspaceFile(page, name);
    await expect(workspace.getByText('音视频预览支持不超过 128 MiB 的文件。', { exact: true })).toBeVisible();
    expect(content).not.toContain(name);
  }
});

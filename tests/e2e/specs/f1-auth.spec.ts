import { test, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { resolve } from 'node:path';
import { authorize } from './helpers';

test('official login exposes the browser link and cancel uses its loginId', async ({page,context}) => {
  await authorize(context);
  // The ChatGPT account signs in on its own card in the provider list.
  await page.goto('/settings/providers');
  await page.getByRole('button',{name:/登录 ChatGPT 账号|Sign in with ChatGPT/i}).click();
  const loginLink=page.getByRole('link',{name:/在浏览器中打开登录页面|Open sign-in page in browser/i});
  await expect(loginLink).toHaveAttribute('href','https://example.invalid/login');
  await page.getByRole('button',{name:/取消登录|Cancel sign-in/i}).click();
  await expect(loginLink).toBeHidden();
});

test('Vite registers the dev authorization route against the real core', async ({browser}) => {
  const root=resolve(import.meta.dirname,'../../..');
  const reservation=createServer();
  await new Promise<void>((done)=>reservation.listen(0,'127.0.0.1',done));
  const port=(reservation.address() as {port:number}).port;
  await new Promise<void>((done,fail)=>reservation.close((error)=>error?fail(error):done()));
  const baseURL=`http://127.0.0.1:${port}`;
  const child=spawn(process.execPath,[resolve(root,'frontend/node_modules/vite/bin/vite.js'),'--host','127.0.0.1','--port',String(port),'--strictPort'],{
    cwd:resolve(root,'frontend'),windowsHide:true,stdio:'ignore',
    env:{...process.env,COBOT_API_ENDPOINT:process.env.COBOT_E2E_ENDPOINT!,COBOT_ADMIN_SECRET:process.env.COBOT_E2E_ADMIN_SECRET!},
  });
  const context=await browser.newContext();
  try{
    await expect.poll(async()=>{try{return (await context.request.post(`${baseURL}/__cobot/dev-link`,{data:{}})).status()}catch{return 0}}).toBe(200);
    const bad=await context.request.post(`${baseURL}/__cobot/dev-link`,{headers:{Origin:'https://untrusted.invalid'},data:{}});
    expect(bad.status()).toBe(403);
  } finally{await context.close();child.kill()}
});

test('a fresh bootstrap works with a stale profile cookie',async({page,context})=>{
  const endpoint=process.env.COBOT_E2E_ENDPOINT!;
  const status=await context.request.get(endpoint+'/api/v1/status');
  const names=(await status.json()).data;
  await context.addCookies([{name:names.sessionCookieName,value:'prior-expired-session',url:endpoint,httpOnly:true,sameSite:'Strict'}]);
  await authorize(context);
  await page.goto('/');
  await expect.poll(async()=>((await (await context.request.get(endpoint+'/api/v1/status')).json()).data.authorized)).toBe(true);
  await expect(page.getByRole('button',{name:/^设置$|^settings$/i}).first()).toBeVisible();
});

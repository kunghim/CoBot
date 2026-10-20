import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export function checkPrRoute(event) {
  const pr = event.pull_request;
  if (!pr) throw new Error('Missing pull request metadata');
  if (pr.base.ref === 'dev') return;
  if (pr.base.ref === 'main' && pr.head.ref === 'dev' && pr.head.repo?.full_name?.toLowerCase() === pr.base.repo?.full_name?.toLowerCase() && pr.head.repo?.full_name) return;
  if (pr.base.ref === 'main' && pr.user?.login === 'dependabot[bot]' && pr.head.repo?.full_name?.toLowerCase() === pr.base.repo?.full_name?.toLowerCase() && pr.head.repo?.full_name) return;
  throw new Error('Target dev for contributions. Main accepts this repository\'s dev promotion and Dependabot security updates. / 普通贡献请向 dev 提交；main 只接受本仓库 dev 的晋级 PR 和 Dependabot 安全更新。');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.env.GITHUB_EVENT_NAME === 'pull_request') {
      checkPrRoute(JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8')));
      console.log('PR route passed.');
    } else console.log('PR route: not a pull request event.');
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

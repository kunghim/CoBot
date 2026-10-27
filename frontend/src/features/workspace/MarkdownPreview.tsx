import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { EmptyState, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import Markdown from '../conversations/Markdown';
import { scopeKey } from './fileNavigation';

export default function MarkdownPreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const content = useQuery({ queryKey: ['workspace-markdown', scopeKey(scope), file.path, file.version], queryFn: async ({ signal }) => (await api.workspaceBytes(scope, file.path, signal, undefined, file.version)).text(), enabled: file.size <= 1024 * 1024, retry: false, gcTime: 30_000 });
  if (file.size > 1024 * 1024) return <EmptyState title={t('workspace.files.richTooLarge')} />;
  if (content.isError) return <EmptyState title={apiErrorMessage(t, content.error)} />;
  if (content.isPending) return <Spinner label={t('common.loading')} />;
  return <div className="h-full overflow-auto p-5 text-sm leading-6"><Markdown text={content.data} teamId={scope.kind === 'team' ? scope.id : undefined} chatId={scope.kind === 'chat' ? scope.id : undefined} basePath={file.path} /></div>;
}

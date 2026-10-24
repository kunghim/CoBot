import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { readLocal, savedMode, writeLocal } from '../../app/mode';
import ArchivedTeamsSection from './ArchivedTeamsSection';
import ArchivedChatsSection from './ArchivedChatsSection';
import ArchiveTypeControl from './ArchiveTypeControl';

export default function ArchivedSection() {
  const [params, setParams] = useSearchParams(); const [busy, setBusy] = useState(false);
  const remembered = readLocal('cobot.archived.type', savedMode() === 'work' ? 'team' : 'chat');
  const type = params.has('team') ? 'team' : params.get('type') === 'team' || params.get('type') === 'chat' ? params.get('type')! : remembered === 'team' ? 'team' : 'chat';
  useEffect(() => { writeLocal('cobot.archived.type', type); }, [type]);
  return <div className="space-y-4">
    <ArchiveTypeControl busy={busy} value={type === 'team' ? 'team' : 'chat'} onChange={(value) => { if (!busy) { const next = new URLSearchParams(params); next.set('type', value); next.delete('team'); next.delete('chat'); setParams(next); } }} />
    {type === 'team' ? <ArchivedTeamsSection key="team" onBusyChange={setBusy} /> : <ArchivedChatsSection key="chat" onBusyChange={setBusy} />}
  </div>;
}

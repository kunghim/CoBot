import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { lastChatPath, writeLocal, type AppMode } from './mode';
import { ModeSelector } from './ModeSelector';

export default function ModeSwitch({ value }: { value: AppMode }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  return <ModeSelector value={value} label={t('chat.mode')} chatLabel={t('chat.chat')} workLabel={t('chat.work')} onChange={(mode) => { writeLocal('cobot.mode', mode); navigate(mode === 'chat' ? lastChatPath() : '/'); }} />;
}

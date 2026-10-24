import { useTranslation } from 'react-i18next';
import { SegmentedControl } from '../../components/ui';

export default function ArchiveTypeControl({ value, onChange, busy = false }: { value: 'chat' | 'team'; onChange: (value: 'chat' | 'team') => void; busy?: boolean }) {
  const { t } = useTranslation();
  return <SegmentedControl label={t('archivePage.type')} value={value} onChange={onChange} options={[
    { value: 'chat', label: t('chat.chat'), disabled: busy },
    { value: 'team', label: t('chat.work'), disabled: busy },
  ]} />;
}

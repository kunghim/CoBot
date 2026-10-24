import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

export function greetingPeriod(hour: number) {
  if (hour < 5 || hour >= 23) return 'night';
  if (hour < 9) return 'morning';
  if (hour < 12) return 'forenoon';
  if (hour < 14) return 'noon';
  if (hour < 18) return 'afternoon';
  return 'evening';
}

export default function ChatWelcome({ hasDraft = false, hour }: { hasDraft?: boolean; hour?: number }) {
  const { t } = useTranslation();
  const [period, setPeriod] = useState(() => greetingPeriod(hour ?? new Date().getHours()));
  useEffect(() => {
    if (hasDraft || hour !== undefined) return;
    const update = () => setPeriod(greetingPeriod(new Date().getHours()));
    update();
    const timer = window.setInterval(update, 60_000);
    window.addEventListener('focus', update);
    return () => { window.clearInterval(timer); window.removeEventListener('focus', update); };
  }, [hasDraft, hour]);
  return <h1 data-testid="chat-welcome" className="mb-6 text-2xl font-medium leading-relaxed tracking-tight text-neutral-900 dark:text-neutral-100">{t(`chat.greeting.${period}`)}</h1>;
}

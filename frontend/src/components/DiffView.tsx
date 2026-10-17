// DiffView shows a unified diff with added, removed and hunk lines tinted.
export default function DiffView({ diff, className = 'max-h-80' }: { diff: string; className?: string }) {
  return (
    <pre className={`${className} overflow-auto rounded bg-neutral-50 p-2 font-mono text-[11px] leading-4 dark:bg-neutral-900`}>
      {diff.split('\n').map((line, index) => {
        const tone =
          line.startsWith('+') && !line.startsWith('+++')
            ? 'bg-green-50 text-green-800 dark:bg-green-950/40 dark:text-green-300'
            : line.startsWith('-') && !line.startsWith('---')
              ? 'bg-red-50 text-red-800 dark:bg-red-950/40 dark:text-red-300'
              : line.startsWith('@@')
                ? 'text-blue-600 dark:text-blue-400'
                : 'text-neutral-600 dark:text-neutral-400';
        return (
          <div key={index} className={`whitespace-pre-wrap break-all ${tone}`}>
            {line || ' '}
          </div>
        );
      })}
    </pre>
  );
}

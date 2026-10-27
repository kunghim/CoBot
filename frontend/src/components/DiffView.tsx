// DiffView shows a unified diff with added, removed and hunk lines tinted.
export default function DiffView({ diff, className = 'max-h-80', startLine = 0, maxLines }: { diff: string; className?: string; startLine?: number; maxLines?: number }) {
  let hunk = false;
  const lines: Array<{ line: string; index: number; tone: string }> = [];
  for (const [index, line] of diff.split('\n').entries()) {
    if (maxLines !== undefined && index >= startLine + maxLines) break;
    if (line.startsWith('diff --git ')) hunk = false;
    if (line.startsWith('@@')) hunk = true;
    const tone = line.startsWith('@@') ? 'text-signal-600 dark:text-signal-400'
      : line.startsWith('+') && (hunk || !line.startsWith('+++')) ? 'bg-green-50 text-green-800 dark:bg-green-950/40 dark:text-green-300'
        : line.startsWith('-') && (hunk || !line.startsWith('---')) ? 'bg-red-50 text-red-800 dark:bg-red-950/40 dark:text-red-300'
          : 'text-neutral-700 dark:text-neutral-300';
    if (index >= startLine) lines.push({ line, index, tone });
  }
  return (
    <pre className={`${className} overflow-auto rounded-lg bg-neutral-900/[0.03] p-2 font-mono text-2xs leading-4 dark:bg-black/30`}>
      {lines.map(({ line, index, tone }) => {
        return (
          <div key={index} className={`whitespace-pre-wrap break-all ${tone}`}>
            {line || ' '}
          </div>
        );
      })}
    </pre>
  );
}

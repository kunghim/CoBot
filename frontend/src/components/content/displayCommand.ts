/** The command a member ran, without the shell the engine wrapped it in:
 * `"C:\\...\\pwsh.exe" -Command 'git status'` reads as `git status`. */
export function displayCommand(command: string): string {
  const trimmed = command.trim();
  const wrapped =
    /^"?[^"]*?(?:pwsh|powershell)(?:\.exe)?"?\s+(?:-NoLogo\s+|-NoProfile\s+|-NonInteractive\s+)*-Command\s+([\s\S]+)$/i.exec(trimmed) ??
    /^"?[^"]*?\b(?:bash|zsh|sh)(?:\.exe)?"?\s+-l?c\s+([\s\S]+)$/i.exec(trimmed) ??
    /^"?[^"]*?\bcmd(?:\.exe)?"?\s+\/[cC]\s+([\s\S]+)$/.exec(trimmed);
  if (!wrapped) return trimmed;
  let inner = wrapped[1].trim();
  const quote = inner[0];
  if (inner.length >= 2 && (quote === "'" || quote === '"') && inner.at(-1) === quote) {
    // Inside single quotes a quote is written twice ('it''s').
    inner = quote === "'" ? inner.slice(1, -1).replace(/''/g, "'") : inner.slice(1, -1);
  }
  return inner;
}

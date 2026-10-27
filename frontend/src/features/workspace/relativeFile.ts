import { linkTarget } from '../conversations/links';

export function relativeFile(currentFile: string, target: string): string {
  if (!currentFile || linkTarget(target).kind !== 'local' || /^(?:\/|file:|[a-z]:[\\/])/i.test(target)) return target;
  const directory = currentFile.split('/').slice(0, -1).join('/');
  return directory ? directory + '/' + target : target;
}

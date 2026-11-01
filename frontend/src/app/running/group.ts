import { JobView, QueueFile, QueueView } from '../api/models';
import { formatBytes, formatEta, formatSpeed } from '../format';

// speed : somme des fichiers, ne sert qu'à la durée restante de la file.
export interface Summary { active: number; pending: number; errors: number; bytesLeft: number; speed: number }

export const filePercent = (f: QueueFile): number =>
  f.state === 'done' ? 100 : f.total > 0 ? Math.min(100, Math.round((f.bytes / f.total) * 100)) : 0;

export function summarize(view: QueueView): Summary {
  const s: Summary = { active: 0, pending: 0, errors: 0, bytesLeft: 0, speed: view.speed };
  for (const f of view.jobs.flatMap((j) => j.files)) {
    if (f.state === 'running') s.active++;
    else if (f.state === 'pending') s.pending++;
    else if (f.state === 'error') s.errors++;
    if (f.state === 'running' || f.state === 'pending') s.bytesLeft += Math.max(0, f.total - f.bytes);
  }
  return s;
}

/** Texte d'un fichier seul : en attente, extraction d'un pack ou téléchargement. */
export function fileMeta(f: QueueFile): string {
  if (f.state === 'pending') return `en attente · ${formatBytes(f.total)}`;
  const done = `${filePercent(f)} % · ${formatBytes(f.bytes)} / ${formatBytes(f.total)}`;
  if (f.phase === 'extraction') return `Extraction · ${done}`;
  const eta = formatEta(f.total - f.bytes, f.speed);
  return `${done}${f.speed > 0 ? ' · ' + formatSpeed(f.speed) : ''}${eta ? ' · ' + eta : ''}`;
}

const base = (p: string) => p.slice(p.lastIndexOf('/') + 1);

/** Fichiers actifs puis en attente, chacun avec son job, dans l'ordre de la file. */
export function activeFiles(jobs: JobView[]): { job: JobView; file: QueueFile }[] {
  const rows = jobs.flatMap((job) =>
    job.files.filter((f) => f.state === 'running' || f.state === 'pending').map((file) => ({ job, file })));
  return rows.sort((a, b) => Number(b.file.state === 'running') - Number(a.file.state === 'running'));
}

/** Ce qui distingue un fichier dans son job : « S02 E03 », le dossier de saison d'un pack, le titre d'un morceau ; vide pour un film. */
export function fileLabel(job: JobView, f: QueueFile): string {
  const name = base(f.dest || f.name);
  if (job.kind === 'music') return name.replace(/\.[^.]+$/, '');
  if (job.kind !== 'series') return '';
  // Un pack d'avant la v0.5.0 vise series/<Nom> : sa saison se lit dans le nom de l'archive.
  return /S\d+\.E\d+/i.exec(name)?.[0].replace('.', ' ') ?? /\bS\d+\b/i.exec(name)?.[0] ?? /\bS\d+\b/i.exec(f.name)?.[0].toUpperCase() ?? name;
}

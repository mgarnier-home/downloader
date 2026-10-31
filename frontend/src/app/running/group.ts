import { HistoryEntry, JobView, QueueFile, QueueView } from '../api/models';
import { formatBytes, formatEta } from '../format';

export interface Summary { active: number; pending: number; errors: number; bytesLeft: number; speed: number }
export interface Cell { label: string; state: 'done' | 'running' | 'pending' | 'error'; percent?: number }

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
  return `${done}${eta ? ' · ' + eta : ''}`;
}

const base = (p: string) => p.slice(p.lastIndexOf('/') + 1);

/** Une cellule par fichier, libellée `E01`… d'après la destination (sinon le nom du fichier). */
export function episodeCells(job: JobView): Cell[] {
  return job.files.map((f) => {
    const label = /E(\d+)/i.exec(base(f.dest))?.[0].toUpperCase() ?? base(f.dest || f.name);
    return f.state === 'running' ? { label, state: f.state, percent: filePercent(f) } : { label, state: f.state };
  });
}

/** Progression globale d'un job, en % des octets connus (les fichiers terminés comptent pour 100 %). */
export function jobPercent(job: JobView): number {
  const total = job.files.reduce((n, f) => n + f.total, 0);
  if (total === 0) return Math.round((job.files.filter((f) => f.state === 'done').length / job.files.length) * 100) || 0;
  const got = job.files.reduce((n, f) => n + (f.state === 'done' ? f.total : f.bytes), 0);
  return Math.min(100, Math.round((got / total) * 100));
}

/** Dossier commun des destinations ; le chemin complet pour un fichier seul. */
export function commonPath(dests: string[]): string {
  const paths = dests.filter(Boolean);
  if (paths.length === 0) return '';
  if (paths.length === 1) return paths[0];
  const dirs = paths.map((p) => p.slice(0, p.lastIndexOf('/') + 1).split('/').slice(0, -1));
  const out: string[] = [];
  for (let i = 0; i < dirs[0].length && dirs.every((d) => d[i] === dirs[0][i]); i++) out.push(dirs[0][i]);
  return out.length ? out.join('/') + '/' : '';
}

export interface DoneGroup { title: string; kind: HistoryEntry['kind']; count: number; at: Date; dest: string }

/** Rangés du jour (heure locale), regroupés par titre ; les dates nulles sont ignorées. */
export function doneToday(rows: HistoryEntry[], now = new Date()): DoneGroup[] {
  const groups = new Map<string, HistoryEntry[]>();
  for (const r of rows) {
    const at = new Date(r.finishedAt);
    if (r.state !== 'done' || at.getFullYear() < 2000 || at.toDateString() !== now.toDateString()) continue;
    const key = `${r.kind}|${r.title}`;
    groups.set(key, [...(groups.get(key) ?? []), r]);
  }
  return [...groups.values()].map((g) => ({
    title: g[0].title || g[0].name, kind: g[0].kind, count: g.length,
    at: new Date(g[0].finishedAt), dest: commonPath(g.map((r) => r.dest)),
  }));
}

import { AnalyzeRow, EnqueueRequest, Kind } from '../api/models';

export interface EditableRow extends AnalyzeRow {
  checked: boolean;
  /** Bonus de série : rangé dans S<ss>/Bonus sous son nom d'origine. */
  bonus?: boolean;
  /** Message du serveur (400 / 409) affiché sur la ligne. */
  message?: string;
}

const pad = (n: number) => String(n).padStart(2, '0');
/** Nom d'œuvre tel qu'affiché : le dossier de l'œuvre dans la destination. */
export const workName = (dest: string): string => dest.split('/')[1] ?? '';
const ext = (name: string) => /\.[A-Za-z0-9]{1,5}$/.exec(name)?.[0] ?? '';

/** Case cochée au départ : en ligne, absent de la médiathèque, et une destination à relire si besoin. */
export function toEditable(r: AnalyzeRow): EditableRow {
  return { ...r, checked: r.online && !r.exists && r.dest !== '' };
}

/** Recompose seulement le gabarit VideoPath / BonusPath ; la mise en forme du nom reste côté serveur. */
export function recompose(r: EditableRow): string {
  const name = workName(r.dest) || (r.tmdb?.title ?? r.name.replace(/\.[^.]+$/, '')).replace(/[^\p{L}\p{N}]/gu, '');
  const s = `S${pad(r.season)}`;
  const e = ext(r.name);
  if (r.kind === 'series') {
    return r.bonus
      ? `series/${name}/${s}/Bonus/${r.name}`
      : `series/${name}/${s}/${name}.${s}.E${pad(r.episode)}${e}`;
  }
  return `${r.kind === 'anime' ? 'animes' : 'films'}/${name}/${name}${e}`;
}

/** Une requête par fiche TMDB (ou par nom d'œuvre sans fiche) parmi les lignes cochées et en ligne. */
export function toRequests(rows: EditableRow[]): EnqueueRequest[] {
  const groups = new Map<string, EnqueueRequest>();
  for (const r of rows) {
    if (!r.checked || !r.online || !r.dest || r.kind === '') continue;
    const key = `${r.kind}|${r.tmdb ? 'tmdb:' + r.tmdb.id : 'nom:' + workName(r.dest)}`;
    let req = groups.get(key);
    if (!req) {
      req = { kind: r.kind as Kind, title: r.tmdb?.title ?? workName(r.dest), ref: r.tmdb ? `tmdb:${r.tmdb.id}` : '', files: [], deezer: false };
      groups.set(key, req);
    }
    req.files.push({ input: r.input, name: r.name, size: r.size, dest: r.dest, overwrite: r.exists });
  }
  return [...groups.values()];
}

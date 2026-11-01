import { AnalyzeRow, EnqueueRequest, Kind, TMDBResult } from '../api/models';

export interface EditableRow extends AnalyzeRow {
  checked: boolean;
  /** Bonus de série : rangé dans S<ss>/Bonus sous son nom d'origine. */
  bonus?: boolean;
  /** Message du serveur (400 / 409) affiché sur la ligne. */
  message?: string;
  /** Destination saisie à la main : un changement de fiche ne la recalcule pas. */
  destEdited?: boolean;
}

const pad = (n: number) => String(n).padStart(2, '0');
/** Nom d'œuvre tel qu'affiché : le dossier de l'œuvre dans la destination. */
export const workName = (dest: string): string => dest.split('/')[1] ?? '';
const ext = (name: string) => /\.[A-Za-z0-9]{1,5}$/.exec(name)?.[0] ?? '';

/** Case cochée au départ : en ligne, absent de la médiathèque, et une destination à relire si besoin. */
export function toEditable(r: AnalyzeRow): EditableRow {
  return { ...r, checked: r.online && !r.exists && r.dest !== '' };
}

/** Même règle que le serveur : sans accents, mots en majuscule initiale, collés (œ→oe, æ→ae). */
export function pascalCase(title: string): string {
  return title.replace(/œ/g, 'oe').replace(/Œ/g, 'Oe').replace(/æ/g, 'ae').replace(/Æ/g, 'Ae')
    .normalize('NFD').replace(/\p{M}/gu, '').split(/[^A-Za-z0-9]+/).filter(Boolean)
    .map((w) => w[0].toUpperCase() + w.slice(1)).join('');
}

/** Recompose seulement le gabarit VideoPath / BonusPath ; la mise en forme du nom reste côté serveur. */
export function recompose(r: EditableRow, work?: string): string {
  const name = work || workName(r.dest) || (r.tmdb?.title ?? r.name.replace(/\.[^.]+$/, '')).replace(/[^\p{L}\p{N}]/gu, '');
  const s = `S${pad(r.season)}`;
  const e = ext(r.name);
  if (r.pack) return `series/${name}/${s}`;
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
      req = { kind: r.kind as Kind, title: r.tmdb?.title ?? workName(r.dest), ref: r.tmdb ? `tmdb:${r.tmdb.id}` : '', files: [], deezer: '' };
      groups.set(key, req);
    }
    req.files.push({ input: r.input, name: r.name, size: r.size, dest: r.dest, overwrite: r.exists, source: r.source });
  }
  return [...groups.values()];
}

/** Colonne Source : « AllDebrid », ou « Direct · <hôte> » pour un lien téléchargé tel quel. */
export function via(r: AnalyzeRow): string {
  if (r.source === 'alldebrid') return 'AllDebrid';
  if (r.source !== 'http') return '—';
  try { return `Direct · ${new URL(r.input).hostname}`; } catch { return 'Direct'; }
}

/** Nouvelle destination : « déjà présent » ne valait que pour celle analysée par le serveur. */
export function withDest(r: EditableRow, dest: string): Pick<EditableRow, 'dest' | 'exists'> {
  return { dest, exists: r.exists && dest === r.dest };
}

/** Applique une fiche TMDB : la destination suit le titre, sauf si elle a été saisie à la main. */
export function applyCard(r: EditableRow, card: TMDBResult): EditableRow {
  const next: EditableRow = { ...r, tmdb: card, message: '' };
  if (r.destEdited) return next;
  if (card.animation && !card.tv && !r.pack) next.kind = 'anime';
  if (r.kind !== '' || next.kind !== '') Object.assign(next, withDest(r, recompose(next, pascalCase(card.title))));
  return next;
}

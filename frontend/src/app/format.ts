import { PackReport } from './api/models';

const dec = (n: number, digits: number) =>
  n.toLocaleString('fr-FR', { minimumFractionDigits: digits, maximumFractionDigits: digits });

// Unités décimales (comme l'affichage du NAS) ; une décimale à partir du Go.
export function formatBytes(n: number): string {
  if (n >= 1e12) return `${dec(n / 1e12, 1)} To`;
  if (n >= 1e9) return `${dec(n / 1e9, 1)} Go`;
  if (n >= 1e6) return `${dec(n / 1e6, 0)} Mo`;
  if (n >= 1e3) return `${dec(n / 1e3, 0)} Ko`;
  return `${dec(n, 0)} o`;
}

export const formatSpeed = (bps: number): string => `${formatBytes(bps)}/s`;

// Vide si la vitesse est nulle (durée inconnue).
export function formatEta(bytesLeft: number, bps: number): string {
  if (bps <= 0) return '';
  const s = Math.round(bytesLeft / bps);
  if (s < 60) return `${s} s`;
  const min = Math.round(s / 60);
  if (min < 60) return `${min} min`;
  return `${Math.floor(min / 60)} h ${String(min % 60).padStart(2, '0')}`;
}

// Libellé et classe CSS de la pastille de type (série, film, animé, musique).
export const KIND_LABEL: Record<string, string> = { series: 'Série', movie: 'Film', anime: 'Animé', music: 'Album' };
export const KIND_CLASS: Record<string, string> = { series: 'serie', movie: 'film', anime: 'anime', music: 'musique' };

/** Message `{"error": …}` renvoyé par le backend, ou le texte de secours. */
export const apiError = (e: unknown, fallback: string): string =>
  (e as { error?: { error?: string } })?.error?.error ?? fallback;

/** Bilan d'un pack : « 62 épisodes rangés · 2 déjà présents · 1 bonus » ; les zéros secondaires sont omis. */
export function packSummary(p: PackReport): string {
  const s = (n: number) => (n > 1 ? 's' : '');
  const parts = [`${p.placed} épisode${s(p.placed)} rangé${s(p.placed)}`];
  if (p.present) parts.push(`${p.present} déjà présent${s(p.present)}`);
  if (p.bonus) parts.push(`${p.bonus} bonus`);
  return parts.join(' · ');
}

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
